package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ermilovadina1-byte/otus-spacebattle-go/auth"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: auth-server serve | hash-password")
	}
	switch args[0] {
	case "serve":
		return serve(args[1:])
	case "hash-password":
		if len(args) != 1 {
			return errors.New("hash-password takes no arguments; supply password on stdin")
		}
		input, err := io.ReadAll(io.LimitReader(os.Stdin, 1026))
		if err != nil {
			return err
		}
		password := strings.TrimSuffix(strings.TrimSuffix(string(input), "\n"), "\r")
		hash, err := auth.HashPassword(password)
		if err != nil {
			return err
		}
		fmt.Println(hash)
		return nil
	default:
		return errors.New("usage: auth-server serve | hash-password")
	}
}

func serve(args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	address := flags.String("listen", "127.0.0.1:8095", "listen address")
	usersPath := flags.String("users", "", "JSON file mapping user IDs to password hashes")
	keyPath := flags.String("private-key", "", "Ed25519 private key in PKCS#8 PEM format")
	certPath := flags.String("tls-cert", "", "TLS certificate for non-loopback listening")
	tlsKeyPath := flags.String("tls-key", "", "TLS private key for non-loopback listening")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *usersPath == "" || *keyPath == "" {
		return errors.New("serve requires -users and -private-key")
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		return err
	}
	if (*certPath == "") != (*tlsKeyPath == "") {
		return errors.New("TLS certificate and key must be supplied together")
	}
	if host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) && *certPath == "" {
		return errors.New("non-loopback listening requires TLS")
	}
	usersJSON, err := os.ReadFile(*usersPath)
	if err != nil {
		return err
	}
	var hashes map[string]string
	if err := json.Unmarshal(usersJSON, &hashes); err != nil {
		return err
	}
	users, err := auth.NewPasswordStore(hashes)
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(*keyPath)
	if err != nil {
		return err
	}
	block, rest := pem.Decode(keyPEM)
	if block == nil || len(rest) != 0 || block.Type != "PRIVATE KEY" {
		return errors.New("expected one PKCS#8 PEM private key")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return err
	}
	privateKey, ok := parsedKey.(ed25519.PrivateKey)
	if !ok {
		return errors.New("private key must use Ed25519")
	}
	issuer, err := auth.NewTokenIssuer(privateKey, 5*time.Minute)
	if err != nil {
		return err
	}
	service, err := auth.NewService(users, auth.NewRegistry(), issuer)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              *address,
		Handler:           service,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       time.Minute,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() {
		if *certPath != "" {
			result <- server.ListenAndServeTLS(*certPath, *tlsKeyPath)
		} else {
			result <- server.ListenAndServe()
		}
	}()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-result; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}
