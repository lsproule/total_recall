package main

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Info struct {
	path     string
	username string
	dbPath   string
	imageStorePath string
	extractionFolder string
}

func buildPath() Info {
	var username string
	fmt.Println("Input username:")
	fmt.Scanln(&username)
	return Info{path: "C:\\Users\\" + username + "\\AppData\\Local\\CoreAIPlatform.00\\UKP", username: username}
}

func checkPath(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func modifyPermissions(I Info) error {
	cmd := exec.Command("icacls", I.path, "/grant", I.username+":(OI)(CI)F", "/T", "/C", "/Q")
	return cmd.Run()
}

func getGUIDFolder(I Info) (string, error) {
	files, err := os.ReadDir(I.path)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if file.IsDir() {
			return file.Name(), nil
		}
	}
	return "", errors.New("no folder found")
}

func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destinationFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destinationFile.Close()

	_, err = io.Copy(destinationFile, sourceFile)
	return err
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relativePath := strings.TrimPrefix(path, src)
		dstPath := filepath.Join(dst, relativePath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copyFile(path, dstPath)
	})
}

func initialize() (Info, error) {
	I := buildPath()

	if !checkPath(I.path) {
		return I, errors.New("base path does not exist")
	}

	if err := modifyPermissions(I); err != nil {
		return I, fmt.Errorf("failed to modify permissions for %s: %v", I.path, err)
	}
	fmt.Printf("Permissions modified for %s and all its subdirectories and files\n", I.path)

	guidFolder, err := getGUIDFolder(I)
	if err != nil {
		return I, errors.New("could not find the GUID folder")
	}
	recallFolder := filepath.Join(I.path, guidFolder)
	fmt.Printf("Recall folder found: %s\n", recallFolder)

	I.dbPath = filepath.Join(recallFolder, "ukg.db")
	I.imageStorePath = filepath.Join(recallFolder, "ImageStore")

	if !checkPath(I.dbPath) || !checkPath(I.imageStorePath) {
		return I, errors.New("windows Recall feature not found. Nothing to extract")
	}

	var proceed string
	fmt.Println("Windows Recall feature found. Do you want to proceed with the extraction? (yes/no): ")
	fmt.Scanln(&proceed)
	if strings.ToLower(proceed) != "yes" {
		return I, errors.New("extraction aborted")
	}

	return I, nil
}

func setupExtractionFolder() (string, error) {
	timestamp := time.Now().Format("2006-01-02-15-04")
	extractionFolder := filepath.Join(".", timestamp+"_Recall_Extraction")
	if err := os.MkdirAll(extractionFolder, os.ModePerm); err != nil {
		return "", fmt.Errorf("failed to create extraction folder: %v", err)
	}
	fmt.Printf("Creating extraction folder: %s\n", extractionFolder)
	return extractionFolder, nil
}

func copyDatabaseAndImages(I Info) error {
	if err := copyFile(I.dbPath, filepath.Join(I.extractionFolder, "ukg.db")); err != nil {
		return fmt.Errorf("failed to copy database file: %v", err)
	}

	if err := copyDir(I.imageStorePath, filepath.Join(I.extractionFolder, "ImageStore")); err != nil {
		return fmt.Errorf("failed to copy image store: %v", err)
	}

	return nil
}

func renameImages(imageStoreExtractionPath string) error {
	files, err := os.ReadDir(imageStoreExtractionPath)
	if err != nil {
		return fmt.Errorf("failed to read image store extraction path: %v", err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".jpg") {
			oldPath := filepath.Join(imageStoreExtractionPath, file.Name())
			newPath := oldPath + ".jpg"
			os.Rename(oldPath, newPath)
		}
	}
	return nil
}

func queryDatabase(dbPath string) ([]string, []string, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT WindowTitle, TimeStamp, ImageToken FROM WindowCapture WHERE (WindowTitle IS NOT NULL OR ImageToken IS NOT NULL)")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query database: %v", err)
	}
	defer rows.Close()

	var capturedWindows, imagesTaken []string
	for rows.Next() {
		var windowTitle string
		var timestamp int64
		var imageToken string
		if err := rows.Scan(&windowTitle, &timestamp, &imageToken); err != nil {
			return nil, nil, fmt.Errorf("failed to scan row: %v", err)
		}
		readableTimestamp := time.Unix(timestamp/1000, 0).Format("2006-01-02 15:04:05")
		if windowTitle != "" {
			capturedWindows = append(capturedWindows, fmt.Sprintf("[%s] %s", readableTimestamp, windowTitle))
		}
		if imageToken != "" {
			imagesTaken = append(imagesTaken, fmt.Sprintf("[%s] %s", readableTimestamp, imageToken))
		}
	}

	return capturedWindows, imagesTaken, nil
}

func writeOutput(extractionFolder string, capturedWindows, imagesTaken []string) error {
	totalRecallFilePath := filepath.Join(extractionFolder, "TotalRecall.txt")
	file, err := os.Create(totalRecallFilePath)
	if err != nil {
		return fmt.Errorf("failed to create output file: %v", err)
	}
	defer file.Close()

	file.WriteString("Captured Windows:\n")
	for _, entry := range capturedWindows {
		file.WriteString(entry + "\n")
	}

	file.WriteString("\nImages Taken:\n")
	for _, entry := range imagesTaken {
		file.WriteString(entry + "\n")
	}

	return nil
}

func main() {
	I, err := initialize()
	if err != nil {
		fmt.Println(err)
		return
	}

	I.extractionFolder, err = setupExtractionFolder()
	if err != nil {
		fmt.Println(err)
		return
	}

	if err := copyDatabaseAndImages(I); err != nil {
		fmt.Println(err)
		return
	}

	if err := renameImages(filepath.Join(I.extractionFolder, "ImageStore")); err != nil {
		fmt.Println(err)
		return
	}

	capturedWindows, imagesTaken, err := queryDatabase(filepath.Join(I.extractionFolder, "ukg.db"))
	if err != nil {
		fmt.Println(err)
		return
	}

	if err := writeOutput(I.extractionFolder, capturedWindows, imagesTaken); err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("\nSummary of the extraction is available in the file: %s\n", filepath.Join(I.extractionFolder, "TotalRecall.txt"))
	fmt.Printf("\nFull extraction folder path: %s\n", I.extractionFolder)
}

