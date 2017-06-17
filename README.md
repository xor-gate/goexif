goexif2
=======

[![License][License-Image]][License-Url]
[![Godoc][Godoc-Image]][Godoc-Url]
[![ReportCard][ReportCard-Image]][ReportCard-Url]
[![Build][Build-Status-Image]][Build-Status-Url]

Provides decoding of basic exif and tiff encoded data. This project is a fork of `rwcarlsen/goexif` with
 many PR and patches integrated.
Suggestions and pull requests are welcome.  

## Installation

To install the exif extraction cli tool, in a terminal type:

```
go install github.com/xor-gate/goexif2/cmd/goexif2
goexif2 <file>.jpg
```

Functionality is split into two packages - "exif" and "tiff"
The exif package depends on the tiff package.

```
go get github.com/xor-gate/goexif2/exif
go get github.com/xor-gate/goexif2/tiff
```

## Example

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/xor-gate/goexif2/exif"
	"github.com/xor-gate/goexif2/mknote"
)

func ExampleDecode() {
	fname := "sample1.jpg"

	f, err := os.Open(fname)
	if err != nil {
		log.Fatal(err)
	}

	// Optionally register camera makenote data parsing - currently Nikon and
	// Canon are supported.
	exif.RegisterParsers(mknote.All...)

	x, err := exif.Decode(f)
	if err != nil {
		log.Fatal(err)
	}

	camModel, _ := x.Get(exif.Model) // normally, don't ignore errors!
	fmt.Println(camModel.StringVal())

	focal, _ := x.Get(exif.FocalLength)
	numer, denom, _ := focal.Rat2(0) // retrieve first (only) rat. value
	fmt.Printf("%v/%v", numer, denom)

	// Two convenience functions exist for date/time taken and GPS coords:
	tm, _ := x.DateTime()
	fmt.Println("Taken: ", tm)

	lat, long, _ := x.LatLong()
	fmt.Println("lat, long: ", lat, ", ", long)
}
```

## License

[2-Clause BSD](LICENSE)

[License-Url]: https://opensource.org/licenses/BSD-2-Clause
[License-Image]: https://img.shields.io/badge/license-2%20Clause%20BSD-blue.svg?maxAge=2592000
[Build-Status-Url]: http://travis-ci.org/xor-gate/goexif2
[Build-Status-Image]: https://travis-ci.org/xor-gate/goexif2.svg?branch=master
[Godoc-Url]: https://godoc.org/github.com/xor-gate/goexif2
[Godoc-Image]: https://godoc.org/github.com/xor-gate/goexif2?status.svg
[ReportCard-Url]: https://goreportcard.com/report/github.com/xor-gate/goexif2
[ReportCard-Image]: https://goreportcard.com/badge/github.com/xor-gate/goexif2
