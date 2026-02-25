package credentials

import "fmt"

var (
	ErrUnsupportedType = fmt.Errorf("unsupported credential type")
	ErrNotFound        = fmt.Errorf("credential not found")
)

type Store interface {
	Get(ref string) (string, error)
	Set(ref, value string) error
	Delete(ref string) error
	Available() (bool, error)
}
