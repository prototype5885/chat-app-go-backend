package main

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

func getAttachmentFolder(fileName string) string {
	shard := fileName[:2]
	return filepath.Join("public", "attachments", shard)
}

func saveAttachment(file multipart.File, originalName string) (string, error) {
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(fileBytes)
	hashStr := hex.EncodeToString(hash[:])

	fileName := hashStr + filepath.Ext(originalName)

	folderPath := getAttachmentFolder(fileName)
	filePath := filepath.Join(folderPath, fileName)

	err = os.MkdirAll(filepath.Dir(filePath), 0755)
	if err != nil {
		return "", err
	}

	err = os.WriteFile(filePath, fileBytes, 0666)
	if err != nil {
		return "", err
	}

	return fileName, nil
}
