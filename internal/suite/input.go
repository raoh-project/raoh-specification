package suite

import (
	"fmt"

	"github.com/raoh-project/raoh-specification/internal/jsontext"
	"github.com/raoh-project/raoh-specification/internal/value"
)

// ValidateInput checks that a JSON value is a value of the input model (spec/input-model.md). The
// syntax is JSON's, which jsontext reads, with the member names and surrogates it refuses; what is
// left is the numbers: each has to be one the input model has, however deep it lies.
func ValidateInput(n *jsontext.Node) error {
	switch n.Kind {
	case jsontext.Number:
		if err := value.CheckNumber(n.Text); err != nil {
			return fmt.Errorf("%s is not a number of the input model: %w", n.Text, err)
		}
	case jsontext.Array:
		for _, e := range n.Elems {
			if err := ValidateInput(e); err != nil {
				return err
			}
		}
	case jsontext.Object:
		for _, m := range n.Members {
			if err := ValidateInput(m.Value); err != nil {
				return err
			}
		}
	}
	return nil
}
