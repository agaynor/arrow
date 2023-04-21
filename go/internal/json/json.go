//go:build !tinygo
// +build !tinygo

package json

import (
	"io"

	"github.com/goccy/go-json"
)

type Decoder = json.Decoder
type Encoder = json.Encoder
type Marshaler = json.Marshaler
type Delim = json.Delim
type UnmarshalTypeError = json.UnmarshalTypeError
type Number = json.Number
type Unmarshaler = json.Unmarshaler
type RawMessage = json.RawMessage

func Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func NewDecoder(r io.Reader) *Decoder {
	return json.NewDecoder(r)
}

func NewEncoder(w io.Writer) *Encoder {
	return json.NewEncoder(w)
}
