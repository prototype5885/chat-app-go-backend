package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"mime/multipart"
	"os"
	"path/filepath"

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
	hashStr := hex.EncodeToString(hash[:])
	fileName := fmt.Sprintf("%s.jpg", hashStr)

	imgPath := filepath.Join("public", "avatars", fileName)

	// lock so multiple goroutines can't write avatar files at same time
	avatarFilesMutex.Lock()
	defer avatarFilesMutex.Unlock()

	// check again after unlock if avatar file exists now
	// other user could have triggered it's creation during lock
	_, err = os.Stat(imgPath)
	if err == nil { // avatar with same hash already exist, no need to write to disk
		return fileName, nil
	} else if !errors.Is(err, os.ErrNotExist) { // unknown error happened during os.Stat
		return "", err
	}

	// create avatars folder if doesn't exist
	err = os.MkdirAll(filepath.Dir(imgPath), 0755)
	if err != nil {
		return "", err
	}

	// save as file
	err = os.WriteFile(imgPath, buf.Bytes(), 0666)
	if err != nil {
		return "", err
	}

	return fileName, nil
}

func generateResizedAvatar(name string, size int) error {
	resizedFilePath := filepath.Join("public", "avatars", fmt.Sprintf("%d", size), name)
	originalFilePath := filepath.Join("public", "avatars", name)

	// lock so multiple goroutines can't resize avatars at same time
	resizedAvatarFilesMutex.Lock()
	defer resizedAvatarFilesMutex.Unlock()

	// check again after unlock if resized avatar file exists now
	// other user could have triggered it's generation during lock
	_, err := os.Stat(resizedFilePath)
	if err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) { // unknown error happened during os.Stat
		return err
	}

	originalFile, err := os.Open(originalFilePath)
	if err != nil {
		return err
	}
	defer originalFile.Close()

	img, _, err := image.Decode(originalFile)
	if err != nil {
		return err
	}

	resizedImg := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(resizedImg, resizedImg.Bounds(), img, img.Bounds(), draw.Src, nil)

	var buf bytes.Buffer
	err = jpeg.Encode(&buf, resizedImg, &jpeg.Options{Quality: 90})
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(resizedFilePath), 0755)
	if err != nil {
		return err
	}

	err = os.WriteFile(resizedFilePath, buf.Bytes(), 0666)
	if err != nil {
		return err
	}

	return nil
}
