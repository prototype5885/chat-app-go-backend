package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"mime/multipart"
	"os"

	"image/jpeg"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/draw"
)

type ImageFormatError struct {
	Err error
}

func (e *ImageFormatError) Error() string {
	return e.Err.Error()
}

func (e *ImageFormatError) Unwrap() error {
	return e.Err
}

func saveAvatar(file multipart.File) (string, error) {
	img, _, err := image.Decode(file)
	if err != nil {
		return "", &ImageFormatError{Err: err}
	}

	// check if width or height is shorter
	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	minSize := min(h, w)

	// crop to center
	x := (w - minSize) / 2
	y := (h - minSize) / 2
	croppedImg := image.NewRGBA(image.Rect(0, 0, minSize, minSize))
	draw.Draw(croppedImg, croppedImg.Bounds(), img, image.Point{x, y}, draw.Src)

	// resize to 256x256
	const size = 256
	resizedImg := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(resizedImg, resizedImg.Bounds(), croppedImg, croppedImg.Bounds(), draw.Src, nil)

	// encode as jpg
	var buf bytes.Buffer
	err = jpeg.Encode(&buf, resizedImg, &jpeg.Options{Quality: 90})
	if err != nil {
		return "", err
	}

	// calculate sha256 hash of avatar
	hash := sha256.Sum256(buf.Bytes())
	imgHashStr := hex.EncodeToString(hash[:])
	fileName := fmt.Sprintf("%s.jpg", imgHashStr)

	// save as file
	err = os.WriteFile(fileName, buf.Bytes(), 0666)
	if err != nil {
		return "", err
	}
	return fileName, nil
}
