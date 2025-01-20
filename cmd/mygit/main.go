package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
)

func main() {
	// You can use print statements as follows for debugging, they'll be visible when running tests.
	fmt.Fprintf(os.Stderr, "Logs from your program will appear here!\n")

	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: mygit <command> [<args>...]\n")
		os.Exit(1)
	}

	switch command := os.Args[1]; command {
	case "init":
		for _, dir := range []string{".git", ".git/objects", ".git/refs"} {
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Fprintf(os.Stderr, "Error creating directory: %s\n", err)
			}
		}

		headFileContents := []byte("ref: refs/heads/main\n")
		if err := os.WriteFile(".git/HEAD", headFileContents, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "Error writing file: %s\n", err)
		}

		fmt.Println("Initialized git directory")

	case "hash-object":
		// check the flag for the command
		if os.Args[2] == "-w" {
			fileToHash := os.Args[3]
			data, err := os.ReadFile(fileToHash)
			if err != nil {
				fmt.Println(" Error reading the file current", err)
			}
			// build it
			toWrite := []byte{}
			toWrite = append(toWrite, []byte("blob ")...)
			toWrite = append(toWrite, byte(len(data)), 0x00)
			toWrite = append(toWrite, data...)

			var b bytes.Buffer
			w := zlib.NewWriter(&b)
			w.Write(toWrite)
			w.Close()

			// what will be written to the hash
			// create the hash
			h := sha1.New()
			h.Write(b.Bytes())
			hashString := fmt.Sprintf("%x", h.Sum(nil))

			dir := ".git/objects/" + string(hashString[0]) + string(hashString[1])
			os.Mkdir(dir, 0755)
			os.WriteFile(dir+"/"+string(hashString[2:]), b.Bytes(), 0755)

		}
	case "cat-file":
		if os.Args[2] == "-p" {
			fileHash := os.Args[3]
			// get the file
			filePath := fmt.Sprintf(".git/objects/%v/%v", string(fileHash[0:2]), string(fileHash[2:]))
			f, err := os.Open(filePath)

			if err != nil {
				fmt.Println("Error could not open file", err)
			}

			defer f.Close()

			r, err := zlib.NewReader(io.Reader(f))
			if err != nil {
				fmt.Println("Error uncompressing file")
			}
			fileContent, err := io.ReadAll(r)
			if err != nil {
				fmt.Println("Error uncompressing file ", err)
			}
			_, blob, found := bytes.Cut(fileContent, []byte{0x00})

			if found {
				fmt.Print(string(blob))
			}
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}
