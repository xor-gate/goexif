goexif2
=======

Provides decoding of basic exif and tiff encoded data. It is written in pure Golang without dependencies to libexif.
 Libexif requires CGO and a C compiler (which is not so easy on windows builds).

This project is a fork of `rwcarlsen/goexif` with many PR and patches integrated. Many people forked the repo
 and did some local changes which where not merged back to the original authors repository. That is why goexif2 
 was born.

## Install ##

```
go get github.com/xor-gate/goexif2
```

## Example ##

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
