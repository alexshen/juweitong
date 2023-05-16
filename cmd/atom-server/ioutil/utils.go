package ioutil

import (
	"fmt"
	"io"
	"io/ioutil"
)

// ConcatFiles writes all files given by paths to the io.Writer w.
func ConcatFiles(w io.Writer, paths ...string) error {
	for _, f := range paths {
		data, err := ioutil.ReadFile(f)
		if err != nil {
			return fmt.Errorf("failed to read %s: %v", f, err)
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}
