package storage

import "bytes"

// bytesReader is a tiny alias so PutBytes can take a slice.
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
