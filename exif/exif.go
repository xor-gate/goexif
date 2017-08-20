// Package exif implements decoding of EXIF data as defined in the EXIF 2.2
// specification (http://www.exif.org/Exif2-2.PDF).
package exif

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/xor-gate/goexif2/tiff"
)

const (
	jpeg_MARKER = 0xFF
	jpeg_APP1   = 0xE1
	jpeg_COM    = 0xFE

	exifPointer    = 0x8769
	gpsPointer     = 0x8825
	interopPointer = 0xA005
)

// A decodeError is returned when the image cannot be decoded as a tiff image.
type decodeError struct {
	cause error
}

func (de decodeError) Error() string {
	return fmt.Sprintf("exif: decode failed (%v) ", de.cause.Error())
}

// IsShortReadTagValueError identifies a ErrShortReadTagValue error.
func IsShortReadTagValueError(err error) bool {
	de, ok := err.(decodeError)
	if ok {
		return de.cause == tiff.ErrShortReadTagValue
	}
	return false
}

var flashDescriptions = map[int]string{
	0x0:  "No Flash",
	0x1:  "Fired",
	0x5:  "Fired, Return not detected",
	0x7:  "Fired, Return detected",
	0x8:  "On, Did not fire",
	0x9:  "On, Fired",
	0xD:  "On, Return not detected",
	0xF:  "On, Return detected",
	0x10: "Off, Did not fire",
	0x14: "Off, Did not fire, Return not detected",
	0x18: "Auto, Did not fire",
	0x19: "Auto, Fired",
	0x1D: "Auto, Fired, Return not detected",
	0x1F: "Auto, Fired, Return detected",
	0x20: "No flash function",
	0x30: "Off, No flash function",
	0x41: "Fired, Red-eye reduction",
	0x45: "Fired, Red-eye reduction, Return not detected",
	0x47: "Fired, Red-eye reduction, Return detected",
	0x49: "On, Red-eye reduction",
	0x4D: "On, Red-eye reduction, Return not detected",
	0x4F: "On, Red-eye reduction, Return detected",
	0x50: "Off, Red-eye reduction",
	0x58: "Auto, Did not fire, Red-eye reduction",
	0x59: "Auto, Fired, Red-eye reduction",
	0x5D: "Auto, Fired, Red-eye reduction, Return not detected",
	0x5F: "Auto, Fired, Red-eye reduction, Return detected",
}

// A TagNotPresentError is returned when the requested field is not
// present in the EXIF.
type TagNotPresentError FieldName

func (tag TagNotPresentError) Error() string {
	return fmt.Sprintf("exif: tag %q is not present", string(tag))
}

func IsTagNotPresentError(err error) bool {
	_, ok := err.(TagNotPresentError)
	return ok
}

// Parser allows the registration of custom parsing and field loading
// in the Decode function.
type Parser interface {
	// Parse should read data from x and insert parsed fields into x via
	// LoadTags.
	Parse(x *Exif) error
}

var parsers []Parser

func init() {
	RegisterParsers(&parser{})
}

// RegisterParsers registers one or more parsers to be automatically called
// when decoding EXIF data via the Decode function.
func RegisterParsers(ps ...Parser) {
	parsers = append(parsers, ps...)
}

type parser struct{}

type tiffErrors map[tiffError]string

func (te tiffErrors) Error() string {
	var allErrors []string
	for k, v := range te {
		allErrors = append(allErrors, fmt.Sprintf("%s: %v\n", stagePrefix[k], v))
	}
	return strings.Join(allErrors, "\n")
}

// IsCriticalError, given the error returned by Decode, reports whether the
// returned *Exif may contain usable information.
func IsCriticalError(err error) bool {
	_, ok := err.(tiffErrors)
	return !ok
}

// IsExifError reports whether the error happened while decoding the EXIF
// sub-IFD.
func IsExifError(err error) bool {
	if te, ok := err.(tiffErrors); ok {
		_, isExif := te[loadExif]
		return isExif
	}
	return false
}

// IsGPSError reports whether the error happened while decoding the GPS sub-IFD.
func IsGPSError(err error) bool {
	if te, ok := err.(tiffErrors); ok {
		_, isGPS := te[loadExif]
		return isGPS
	}
	return false
}

// IsInteroperabilityError reports whether the error happened while decoding the
// Interoperability sub-IFD.
func IsInteroperabilityError(err error) bool {
	if te, ok := err.(tiffErrors); ok {
		_, isInterop := te[loadInteroperability]
		return isInterop
	}
	return false
}

type tiffError int

const (
	loadExif             tiffError = iota
	loadGPS
	loadInteroperability
)

var stagePrefix = map[tiffError]string{
	loadExif:             "loading EXIF sub-IFD",
	loadGPS:              "loading GPS sub-IFD",
	loadInteroperability: "loading Interoperability sub-IFD",
}

// Parse reads data from the tiff data in x and populates the tags
// in x. If parsing a sub-IFD fails, the error is recorded and
// parsing continues with the remaining sub-IFDs.
func (p *parser) Parse(x *Exif) error {
	if len(x.Tiff.Dirs) == 0 {
		return errors.New("Invalid exif data")
	}
	x.LoadTags(x.Tiff.Dirs[0], exifFields, false)

	// thumbnails
	if len(x.Tiff.Dirs) >= 2 {
		x.LoadTags(x.Tiff.Dirs[1], thumbnailFields, false)
	}

	te := make(tiffErrors)

	// recurse into exif, gps, and interop sub-IFDs
	if err := loadSubDir(x, ExifIFDPointer, exifFields); err != nil {
		te[loadExif] = err.Error()
	}
	if err := loadSubDir(x, GPSInfoIFDPointer, gpsFields); err != nil {
		te[loadGPS] = err.Error()
	}

	if err := loadSubDir(x, InteroperabilityIFDPointer, interopFields); err != nil {
		te[loadInteroperability] = err.Error()
	}
	if len(te) > 0 {
		return te
	}
	return nil
}

func loadSubDir(x *Exif, ptr FieldName, fieldMap map[uint16]FieldName) error {
	tag, err := x.Get(ptr)
	if err != nil {
		return nil
	}
	offset, err := tag.Int64(0)
	if err != nil {
		return nil
	}

	_, err = x.rawReader.Seek(offset, 0)
	if err != nil {
		return fmt.Errorf("exif: seek to sub-IFD %s failed: %v", ptr, err)
	}
	subDir, _, err := tiff.DecodeDir(x.rawReader, x.Tiff.Order)
	if err != nil {
		return fmt.Errorf("exif: sub-IFD %s decode failed: %v", ptr, err)
	}
	x.LoadTags(subDir, fieldMap, false)
	return nil
}

// Exif provides access to decoded EXIF metadata fields and values.
type Exif struct {
	Tiff      *tiff.Tiff
	main      map[FieldName]*tiff.Tag
	rawReader tiff.ReadAtReaderSeeker
	// Contents of the JPEG COM segment (Comment).
	Comment string
}

// Decode parses EXIF-encoded data from r and returns a queryable Exif
// object. After the exif data section is called and the tiff structure
// decoded, each registered parser is called (in order of registration). If
// one parser returns an error, decoding terminates and the remaining
// parsers are not called.
// The error can be inspected with functions such as IsCriticalError to
// determine whether the returned object might still be usable.
func Decode(r tiff.ReadAtReaderSeeker) (*Exif, error) {
	// EXIF data in JPEG is stored in the APP1 marker. EXIF data uses the TIFF
	// format to store data.
	// If we're parsing a TIFF image, we don't need to strip away any data.
	// If we're parsing a JPEG image, we need to strip away the JPEG APP1
	// marker and also the EXIF header.

	header := make([]byte, 4)
	n, err := io.ReadFull(r, header)
	if err != nil {
		return nil, err
	}
	if n < len(header) {
		return nil, errors.New("exif: short read on header")
	}

	readOffset := int64(0)
	_, err = r.Seek(readOffset, 0)
	if err != nil {
		return nil, err
	}

	var isTiff bool
	switch string(header) {
	case "II*\x00":
		// TIFF - Little endian (Intel)
		isTiff = true
	case "MM\x00*":
		// TIFF - Big endian (Motorola)
		isTiff = true
	default:
		// Not TIFF, assume JPEG
	}

	var tif *tiff.Tiff
	var comment string
	var rawReader tiff.ReadAtReaderSeeker
	if isTiff {
		// Functions below need the IFDs from the TIFF data to be stored in a
		// *bytes.Reader.  We use TeeReader to get a copy of the bytes as a
		// side-effect of tiff.Decode() doing its work.
		tif, err = tiff.Decode(r)
		if err != nil {
			return nil, decodeError{cause: err}
		}
		rawReader = r
	} else {
		// Locate the JPEG APP1 header.
		var sec *appSec
		sec, err = newAppSec(jpeg_APP1, r, readOffset)
		if err != nil {
			return nil, err
		}

		readOffset = sec.startOffset + int64(sec.dataLength)
		_, err := r.Seek(readOffset, 0)
		if err != nil {
			return nil, err
		}

		var desc *appSec
		desc, err = newAppSec(jpeg_COM, r, readOffset)
		if err == nil {
			buf, err := desc.getBytes(r)
			if err == nil {
				comment = string(buf)
			}
		}
		rawReader, err = sec.exifReader(r)
		if err != nil {
			return nil, decodeError{cause: err}
		}
		tif, err = tiff.Decode(rawReader)
		if err != nil {
			return nil, decodeError{cause: err}
		}
	}

	// build an exif structure from the tiff
	x := &Exif{
		main:      map[FieldName]*tiff.Tag{},
		rawReader: rawReader,
		Tiff:      tif,
		Comment:   comment,
	}

	for i, p := range parsers {
		if err := p.Parse(x); err != nil {
			if _, ok := err.(tiffErrors); ok {
				return x, err
			}
			// This should never happen, as Parse always returns a tiffError
			// for now, but that could change.
			return x, fmt.Errorf("exif: parser %v failed (%v)", i, err)
		}
	}

	return x, nil
}

// LoadTags loads tags into the available fields from the tiff Directory
// using the given tagid-fieldname mapping.  Used to load makernote and
// other meta-data.  If showMissing is true, tags in d that are not in the
// fieldMap will be loaded with the FieldName UnknownPrefix followed by the
// tag ID (in hex format).
func (x *Exif) LoadTags(d *tiff.Dir, fieldMap map[uint16]FieldName, showMissing bool) {
	for _, tag := range d.Tags {
		name := fieldMap[tag.Id]
		if name == "" {
			if !showMissing {
				continue
			}
			name = FieldName(fmt.Sprintf("%v%x", UnknownPrefix, tag.Id))
		}
		x.main[name] = tag
	}
}

// Get retrieves the EXIF tag for the given field name.
//
// If the tag is not known or not present, an error is returned. If the
// tag name is known, the error will be a TagNotPresentError.
func (x *Exif) Get(name FieldName) (*tiff.Tag, error) {
	if tg, ok := x.main[name]; ok {
		return tg, nil
	}
	return nil, TagNotPresentError(name)
}

// Walker is the interface used to traverse all fields of an Exif object.
type Walker interface {
	// Walk is called for each non-nil EXIF field. Returning a non-nil
	// error aborts the walk/traversal.
	Walk(name FieldName, tag *tiff.Tag) error
}

// WalkerFunc is an adapter to allow the use of ordinary functions for Walk.
type WalkerFunc func(name FieldName, tag *tiff.Tag) error

// Walk calls f(name, tag)
func (f WalkerFunc) Walk(name FieldName, tag *tiff.Tag) error {
	return f(name, tag)
}

// Walk calls the Walk method of w with the name and tag for every non-nil
// EXIF field.  If w aborts the walk with an error, that error is returned.
func (x *Exif) Walk(w Walker) error {
	for name, tag := range x.main {
		if err := w.Walk(name, tag); err != nil {
			return err
		}
	}
	return nil
}

// DateTime returns the EXIF's "DateTimeOriginal" field, which
// is the creation time of the photo. If not found, it tries
// the "DateTime" (which is meant as the modtime) instead.
// The error will be TagNotPresentErr if none of those tags
// were found, or a generic error if the tag value was
// not a string, or the error returned by time.Parse.
//
// If the EXIF lacks timezone information or GPS time, the returned
// time's Location will be time.Local.
func (x *Exif) DateTime() (time.Time, error) {
	var dt time.Time
	tag, err := x.Get(DateTimeOriginal)
	if err != nil {
		tag, err = x.Get(DateTime)
		if err != nil {
			return dt, err
		}
	}
	if tag.Format() != tiff.StringVal {
		return dt, errors.New("DateTime[Original] not in string format")
	}
	exifTimeLayout := "2006:01:02 15:04:05"
	dateStr := strings.TrimRight(string(tag.Val), "\x00")
	// TODO(bradfitz,mpl): look for timezone offset, GPS time, etc.
	timeZone := time.Local
	if tz, _ := x.TimeZone(); tz != nil {
		timeZone = tz
	}
	return time.ParseInLocation(exifTimeLayout, dateStr, timeZone)
}

func (x *Exif) TimeZone() (*time.Location, error) {
	// TODO: parse more timezone fields (e.g. Nikon WorldTime).
	timeInfo, err := x.Get("Canon.TimeInfo")
	if err != nil {
		return nil, err
	}
	if timeInfo.Count < 2 {
		return nil, errors.New("Canon.TimeInfo does not contain timezone")
	}
	offsetMinutes, err := timeInfo.Int(1)
	if err != nil {
		return nil, err
	}
	return time.FixedZone("", offsetMinutes*60), nil
}

func ratFloat(num, dem int64) float64 {
	return float64(num) / float64(dem)
}

// Tries to parse a Geo degrees value from a string as it was found in some
// EXIF data.
// Supported formats so far:
// - "52,00000,50,00000,34,01180" ==> 52 deg 50'34.0118"
//   Probably due to locale the comma is used as decimal mark as well as the
//   separator of three floats (degrees, minutes, seconds)
//   http://en.wikipedia.org/wiki/Decimal_mark#Hindu.E2.80.93Arabic_numeral_system
// - "52.0,50.0,34.01180" ==> 52deg50'34.0118"
// - "52,50,34.01180"     ==> 52deg50'34.0118"
func parseTagDegreesString(s string) (float64, error) {
	const unparsableErrorFmt = "Unknown coordinate format: %s"
	isSplitRune := func(c rune) bool {
		return c == ',' || c == ';'
	}
	parts := strings.FieldsFunc(s, isSplitRune)
	var degrees, minutes, seconds float64
	var err error
	switch len(parts) {
	case 6:
		degrees, err = strconv.ParseFloat(parts[0]+"."+parts[1], 64)
		if err != nil {
			return 0.0, fmt.Errorf(unparsableErrorFmt, s)
		}
		minutes, err = strconv.ParseFloat(parts[2]+"."+parts[3], 64)
		if err != nil {
			return 0.0, fmt.Errorf(unparsableErrorFmt, s)
		}
		minutes = math.Copysign(minutes, degrees)
		seconds, err = strconv.ParseFloat(parts[4]+"."+parts[5], 64)
		if err != nil {
			return 0.0, fmt.Errorf(unparsableErrorFmt, s)
		}
		seconds = math.Copysign(seconds, degrees)
	case 3:
		degrees, err = strconv.ParseFloat(parts[0], 64)
		if err != nil {
			return 0.0, fmt.Errorf(unparsableErrorFmt, s)
		}
		minutes, err = strconv.ParseFloat(parts[1], 64)
		if err != nil {
			return 0.0, fmt.Errorf(unparsableErrorFmt, s)
		}
		minutes = math.Copysign(minutes, degrees)
		seconds, err = strconv.ParseFloat(parts[2], 64)
		if err != nil {
			return 0.0, fmt.Errorf(unparsableErrorFmt, s)
		}
		seconds = math.Copysign(seconds, degrees)
	default:
		return 0.0, fmt.Errorf(unparsableErrorFmt, s)
	}
	return degrees + minutes/60.0 + seconds/3600.0, nil
}

func parse3Rat2(tag *tiff.Tag) ([3]float64, error) {
	v := [3]float64{}
	for i := range v {
		num, den, err := tag.Rat2(i)
		if err != nil {
			return v, err
		}
		v[i] = ratFloat(num, den)
		if tag.Count < uint32(i+2) {
			break
		}
	}
	return v, nil
}

func tagDegrees(tag *tiff.Tag) (float64, error) {
	switch tag.Format() {
	case tiff.RatVal:
		// The usual case, according to the Exif spec
		// (http://www.kodak.com/global/plugins/acrobat/en/service/digCam/exifStandard2.pdf,
		// jpegSec 4.6.6, p. 52 et seq.)
		v, err := parse3Rat2(tag)
		if err != nil {
			return 0.0, err
		}
		return v[0] + v[1]/60 + v[2]/3600.0, nil
	case tiff.StringVal:
		// Encountered this weird case with a panorama picture taken with a HTC phone
		s, err := tag.StringVal()
		if err != nil {
			return 0.0, err
		}
		return parseTagDegreesString(s)
	default:
		// don't know how to parse value, give up
		return 0.0, fmt.Errorf("Malformed EXIF Tag Degrees")
	}
}

// LatLong returns the latitude and longitude of the photo and
// whether it was present.
func (x *Exif) LatLong() (lat, long float64, err error) {
	// All calls of x.Get might return an TagNotPresentError
	longTag, err := x.Get(FieldName("GPSLongitude"))
	if err != nil {
		return
	}
	ewTag, err := x.Get(FieldName("GPSLongitudeRef"))
	if err != nil {
		return
	}
	latTag, err := x.Get(FieldName("GPSLatitude"))
	if err != nil {
		return
	}
	nsTag, err := x.Get(FieldName("GPSLatitudeRef"))
	if err != nil {
		return
	}
	if long, err = tagDegrees(longTag); err != nil {
		return 0, 0, fmt.Errorf("Cannot parse longitude: %v", err)
	}
	if lat, err = tagDegrees(latTag); err != nil {
		return 0, 0, fmt.Errorf("Cannot parse latitude: %v", err)
	}
	if math.Abs(long) > 180.0 {
		return 0, 0, fmt.Errorf("Longitude outside allowed range: %v", long)
	}
	if math.Abs(lat) > 90.0 {
		return 0, 0, fmt.Errorf("Latitude outside allowed range: %v", lat)
	}
	ew, err := ewTag.StringVal()
	if err == nil && ew == "W" {
		long *= -1.0
	} else if err != nil {
		return 0, 0, fmt.Errorf("Cannot parse longitude: %v", err)
	}
	ns, err := nsTag.StringVal()
	if err == nil && ns == "S" {
		lat *= -1.0
	} else if err != nil {
		return 0, 0, fmt.Errorf("Cannot parse longitude: %v", err)
	}
	return lat, long, nil
}

// String returns a pretty text representation of the decoded exif data.
func (x *Exif) String() string {
	var buf bytes.Buffer
	for name, tag := range x.main {
		fmt.Fprintf(&buf, "%s: %s\n", name, tag)
	}
	return buf.String()
}

// JpegThumbnail returns the jpeg thumbnail if it exists. If it doesn't exist,
// TagNotPresentError will be returned
func (x *Exif) JpegThumbnail() ([]byte, error) {
	return x.getBytesFromTagOffsets(ThumbJPEGInterchangeFormat, ThumbJPEGInterchangeFormatLength)
}

// PreviewImage returns the preview image if it exists. If it doesn't exist,
// TagNotPresentError will be returned
func (x *Exif) PreviewImage() ([]byte, error) {
	return x.getBytesFromTagOffsets(PreviewImageStart, PreviewImageLength)
}

// JpegFromRaw returns the jpeg from raw image if it exists. If it doesn't exist,
// TagNotPresentError will be returned
func (x *Exif) JpegFromRaw() ([]byte, error) {
	return x.getBytesFromTagOffsets(JpegFromRawFormat, JpegFromRawFormatLength)
}

// getBytesFromTagOffsets returns the bytes specified by the given start and length tag, if they exist.
func (x *Exif) getBytesFromTagOffsets(startTagField, lengthTagField FieldName) ([]byte, error) {
	startTag, err := x.Get(startTagField)
	if err != nil {
		return nil, err
	}
	start, err := startTag.Int(0)
	if err != nil {
		return nil, err
	}

	lengthTag, err := x.Get(lengthTagField)
	if err != nil {
		return nil, err
	}
	length, err := lengthTag.Int(0)
	if err != nil {
		return nil, err
	}

	_, err = x.rawReader.Seek(int64(start), 0)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, length)
	_, err = io.ReadFull(x.rawReader, buf)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

// MarshalJson implements the encoding/json.Marshaler interface providing output of
// all EXIF fields present (names and values).
func (x Exif) MarshalJSON() ([]byte, error) {
	return json.Marshal(x.main)
}

type appSec struct {
	marker      byte
	startOffset int64
	dataLength  int
}

// newAppSec finds marker in r and returns the corresponding application data
// section.
func newAppSec(marker byte, r io.ReadSeeker, startOffset int64) (*appSec, error) {
	app := &appSec{
		marker:      marker,
		startOffset: startOffset,
	}

	buf := make([]byte, 32*1024)
	prevWasMarker := false
	// seek to marker
ReadLoop:
	for {
		_, err := io.ReadFull(r, buf)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return nil, err
		}

		for i := range buf {
			app.startOffset++

			if prevWasMarker && buf[i] == marker {
				// Marker found
				break ReadLoop
			}

			prevWasMarker = buf[i] == jpeg_MARKER
		}
		// If the ReadFull returned EOF, return
		if err != nil {
			return nil, err
		}
	}

	dataLenBytes := make([]byte, 2)
	r.Seek(startOffset, 0)
	_, err := io.ReadFull(r, dataLenBytes)
	if err != nil {
		return nil, err
	}
	app.dataLength = int(binary.BigEndian.Uint16(dataLenBytes)) - 2
	if app.dataLength <= 0 {
		return nil, errors.New("jpeg section: invalid data length")
	}
	app.startOffset += 2 // Add 2 to skip the length bytes
	// Offset and length set
	return app, nil
}

var exifMarker = append([]byte("Exif"), 0x00, 0x00)

func (app *appSec) exifReader(r tiff.ReadAtReaderSeeker) (tiff.ReadAtReaderSeeker, error) {
	_, err := r.Seek(app.startOffset, 0)
	if err != nil {
		return nil, err
	}

	headerBuf := make([]byte, 6)
	n, err := io.ReadFull(r, headerBuf)
	if err != nil {
		return nil, err
	}

	// Skip the 2 marker bytes in comparison
	if n < 6 || !bytes.Equal(headerBuf, exifMarker) {
		return nil, errors.New("exif: failed to find exif intro marker")
	}

	rdr := io.NewSectionReader(r, app.startOffset+6, int64(app.dataLength)-6)
	return rdr, nil
}

func (s *appSec) getBytes(r io.ReadSeeker) ([]byte, error) {
	buf := make([]byte, s.dataLength)
	_, err := r.Seek(s.startOffset, 0)
	if err != nil {
		return buf, err
	}

	_, err = io.ReadFull(r, buf)
	return buf, err
}

// Flash returns the descriptive text that corresponds to the flash value of the
// photo if it is present.
func (x *Exif) Flash() (string, error) {
	flashTag, err := x.Get(FieldName("Flash"))
	if err != nil {
		return "", err
	}
	flashVal, err := flashTag.Int(0)
	if err != nil {
		return "", err
	}
	return flashDescriptions[flashVal], nil
}
