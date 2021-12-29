package transaction

import (
	"github.com/nspcc-dev/neo-go/pkg/io"
)

// Reserved represents an attribute for experimental or private usage.
type Reserved struct {
	Value []byte
}

// DecodeBinary implements io.Serializable interface.
func (e *Reserved) DecodeBinary(br *io.BinReader) {
	e.Value = br.ReadVarBytes()
}

// EncodeBinary implements io.Serializable interface.
func (e *Reserved) EncodeBinary(w *io.BinWriter) {
	w.WriteVarBytes(e.Value)
}

func (e *Reserved) toJSONMap(m map[string]interface{}) {
	m["value"] = e.Value
}
