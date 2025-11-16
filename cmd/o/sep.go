package main

import "io"

func writeSep(w io.Writer) error {
	_, err := w.Write([]byte("---\n"))
	return err
}
