// Package tiff implements TIFF decoding as defined in TIFF 6.0 specification at
// http://partners.adobe.com/public/developer/en/tiff/TIFF6.pdf
package tiff

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

// ReadAtReader is used when decoding Tiff tags and directories
type ReadAtReader interface {
	io.Reader
	io.ReaderAt
}

type ReadAtReaderSeeker interface {
	ReadAtReader
	io.Seeker
}

type TiffError struct {
	Message string
	// Underlying error
	Err error
}

func newTiffError(msg string, err error) TiffError {
	return TiffError{
		Message: msg,
		Err:     err,
	}
}

func (t TiffError) Error() string {
	if t.Err == nil {
		return fmt.Sprintf("tiff: %s", t.Message)
	} else {
		return fmt.Sprintf("tiff: %s: %s", t.Message, t.Err.Error())
	}
}

// Tiff provides access to a decoded tiff data structure.
type Tiff struct {
	// Dirs is an ordered slice of the tiff's Image File Directories (IFDs).
	// The IFD at index 0 is IFD0.
	Dirs []*Dir
	// The tiff's byte-encoding (i.e. big/little endian).
	Order binary.ByteOrder
}

// Decode parses tiff-encoded data from r and returns a Tiff struct that
// reflects the structure and content of the tiff data. The first read from r
// should be the first byte of the tiff-encoded data and not necessarily the
// first byte of an os.File object.
func Decode(r ReadAtReaderSeeker) (*Tiff, error) {
	t := new(Tiff)

	// read byte order
	bo := make([]byte, 2)
	_, err := io.ReadFull(r, bo)
	if err != nil {
		return nil, newTiffError("could not read tiff byte order", err)
	}
	if string(bo) == "II" {
		t.Order = binary.LittleEndian
	} else if string(bo) == "MM" {
		t.Order = binary.BigEndian
	} else {
		return nil, newTiffError("could not read tiff byte order", nil)
	}

	// check for special tiff marker
	var sp int16
	err = binary.Read(r, t.Order, &sp)
	if err != nil || 42 != sp {
		return nil, newTiffError("could not find special tiff marker", err)
	}

	// load offset to first IFD
	var offset int32
	err = binary.Read(r, t.Order, &offset)
	if err != nil {
		return nil, newTiffError("could not read offset to first IFD", err)
	}

	// load IFD's
	var d *Dir
	prev := offset
	for offset != 0 {
		// seek to offset
		_, err := r.Seek(int64(offset), 0)
		if err != nil {
			return nil, newTiffError("seek to IFD failed", err)
		}

		// load the dir
		d, offset, err = DecodeDir(r, t.Order)
		if err != nil {
			if e, ok := err.(TiffError); ok && e.Err == io.EOF {
				// Previous IFD had a pointer outside of the file. Ignore
				continue
			}
			return nil, err
		}

		if offset == prev {
			return nil, newTiffError("recursive IFD", nil)
		}
		prev = offset

		t.Dirs = append(t.Dirs, d)
	}

	return t, nil
}

func (tf *Tiff) String() string {
	var buf bytes.Buffer
	fmt.Fprint(&buf, "Tiff{")
	for _, d := range tf.Dirs {
		fmt.Fprintf(&buf, "%s, ", d.String())
	}
	fmt.Fprintf(&buf, "}")
	return buf.String()
}

// Dir provides access to the parsed content of a tiff Image File Directory (IFD).
type Dir struct {
	Tags []*Tag
}

// DecodeDir parses a tiff-encoded IFD from r and returns a Dir object.  offset
// is the offset to the next IFD.  The first read from r should be at the first
// byte of the IFD. ReadAt offsets should generally be relative to the
// beginning of the tiff structure (not relative to the beginning of the IFD).
func DecodeDir(r ReadAtReader, order binary.ByteOrder) (d *Dir, offset int32, err error) {
	d = new(Dir)

	// get num of tags in ifd
	var nTags int16
	err = binary.Read(r, order, &nTags)
	if err != nil {
		return nil, 0, newTiffError("failed to read IFD tag count", err)
	}

	// load tags
	for n := 0; n < int(nTags); n++ {
		t, err := DecodeTag(r, order)
		if err == errUnhandledTagType {
			continue
		} else if err != nil {
			return nil, 0, err
		}
		d.Tags = append(d.Tags, t)
	}

	// get offset to next ifd
	err = binary.Read(r, order, &offset)
	if err != nil {
		return nil, 0, newTiffError("failed to read offset to next IFD", err)
	}

	return d, offset, nil
}

func (d *Dir) String() string {
	s := "Dir{"
	for _, t := range d.Tags {
		s += t.String() + ", "
	}
	return s + "}"
}
