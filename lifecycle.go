package spacebattle

// FinishingObject demonstrates an operation that does not return a value.
// Go still returns an error so failure is visible to the caller.
//
//go:generate go run ./cmd/adaptergen -type FinishingObject -output finishing_object_adapter_gen.go
type FinishingObject interface {
	Finish() error
}
