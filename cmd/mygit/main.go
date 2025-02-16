package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type hashType uint32

const (
	TREE hashType = 40000
	BLOB hashType = 100644
	EXEC          = 100755
	SYM           = 120000
)

func (h hashType) String() string {
	switch h {
	case 40000:
		return fmt.Sprintf("0%d %s", h, "tree")
	case 100644:
		return fmt.Sprintf("%d %s", h, "blob")
	case 100755:
		return fmt.Sprintf("%d %s", h, "executable")
	case 120000:
		return fmt.Sprintf("%d %s", h, "symlink")
	default:
		return fmt.Sprintf("%d %s", h, "unknown")
	}
}

type TreeEntry struct {
	mode hashType
	hash string
	name string
}

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
			fileData, err := os.ReadFile(fileToHash)
			if err != nil {
				fmt.Println(" Error reading the file current", err)
			}
			objectToWrite := []byte(fmt.Sprintf("blob %d\x00", len(fileData)))
			objectToWrite = append(objectToWrite, fileData...)

			var b bytes.Buffer
			w := zlib.NewWriter(&b)
			w.Write(objectToWrite)
			w.Close()

			// what will be written to the hash
			// create the hash
			h := sha1.New()
			h.Write(objectToWrite)
			hashString := fmt.Sprintf("%x", h.Sum(nil))

			dir := ".git/objects/" + string(hashString[0]) + string(hashString[1])
			os.Mkdir(dir, 0755)
			os.WriteFile(dir+"/"+string(hashString[2:]), b.Bytes(), 0755)

			fmt.Println(hashString)

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
	case "ls-tree":
		var treeHash string
		if os.Args[2] == "--name-only" {
			treeHash = os.Args[3]
		} else {
			treeHash = os.Args[2]
		}
		filePath := fmt.Sprintf(".git/objects/%v/%v", string(treeHash[0:2]), string(treeHash[2:]))
		treeFile, err := os.Open(filePath)
		if err != nil {
			fmt.Println("could not open tree hash", err)
		}
		f, _ := zlib.NewReader(treeFile)
		treeContent, err := io.ReadAll(f)
		if err != nil {
			fmt.Println("Error reading decompressed file")
		}
		// cut treeHeader
		_, treeEntries, _ := bytes.Cut(treeContent, []byte{0x00})

		remainingEnteries := treeEntries
		entries := []TreeEntry{}

		for {
			entry := new(TreeEntry)
			entryDes, remaining, found := bytes.Cut(remainingEnteries, []byte{0x00})
			if !found {
				break
			}
			v := strings.Split(string(entryDes), " ")
			mode, _ := strconv.Atoi(v[0])
			entry.mode = hashType(mode)
			entry.name = v[1]
			// get the first 20 bytes which is the hash
			entry.hash = fmt.Sprintf("%x", remaining[:20])
			remainingEnteries = remaining[20:]
			entries = append(entries, *entry)
		}
		if os.Args[2] == "--name-only" {
			for _, entry := range entries {
				fmt.Println(entry.name)
			}
		}

	case "write-tree":
		// walk the repository to get the files and folders
		var walkFiles func(path string) []byte
		walkFiles = func(path string) []byte {
			treeEntries := []byte{}
			entries, err := os.ReadDir(path)
			if err != nil {
				fmt.Println("could not read directory")
			}
			for _, entry := range entries {
				if entry.Name() == ".git" {
					continue
				}
				if !entry.IsDir() {
					// open the file
					fileContent, err := os.ReadFile(path + "/" + entry.Name())
					if err != nil {
						fmt.Println("Could not open this file ", path+entry.Name())
						continue
					}
					fileToWrite := fmt.Sprintf("blob %d\x00%s", len(fileContent), fileContent)
					hash := writeObject([]byte(fileToWrite))

					treeEntries = append(treeEntries, []byte(fmt.Sprintf("%d %s\x00", BLOB, entry.Name()))...)
					treeEntries = append(treeEntries, hash[:]...)
				}

				if entry.IsDir() {
					treeEntries = append(treeEntries, walkFiles(filepath.Join(path, entry.Name()))...)
				}
			}
			// write the tree
			treeContent := []byte(fmt.Sprintf("tree %d\x00", len(treeEntries)))
			treeContent = append(treeContent, treeEntries...)

			hash := writeObject(treeContent)

			treeEntry := []byte(fmt.Sprintf("%d %s\x00", TREE, filepath.Base(path)))
			treeEntry = append(treeEntry, hash[:]...)

			return treeEntry
		}
		treeHash := walkFiles(".")
		_, hash, _ := bytes.Cut(treeHash, []byte{0x00})
		fmt.Printf("%x", hash)
	case "commit-tree":
		treeHash := os.Args[2]
		var parentCommit string
		var commitMessage string
		if os.Args[3] == "-p" {
			parentCommit = os.Args[4]
			commitMessage = os.Args[6]
		} else if os.Args[3] == "-m" {
			commitMessage = os.Args[4]
		}
		contentBody := fmt.Sprintf("tree %s\n", treeHash)
		if parentCommit != "" {
			contentBody = fmt.Sprintf("%sparent %s\n", contentBody, parentCommit)
		}
		t := time.Now()
		_, offset := t.Zone()
		author := "Frantoti <fatoti@gmail.com>"
		contentBody = fmt.Sprintf("%sauthor %s %d %d\n", contentBody, author, t.Unix(), offset)
		contentBody = fmt.Sprintf("%scommitter %s %d %d\n", contentBody, author, t.Unix(), offset)
		contentBody = fmt.Sprintf("%s\n%s\n", contentBody, commitMessage)

		content := []byte(fmt.Sprintf("commit %d\x00", len(contentBody)))
		content = append(content, []byte(contentBody)...)

		hash := writeObject(content)

		fmt.Printf("%x", hash[:])
	case "clone":

	default:
		fmt.Fprintf(os.Stderr, "Unknown command %s\n", command)
		os.Exit(1)
	}
}

func writeObject(content []byte) []byte {
	hash := sha1.Sum([]byte(content))
	hashString := hex.EncodeToString(hash[:])
	storeDir := fmt.Sprintf("%s/%s", ".git/objects", hashString[0:2])

	if err := os.MkdirAll(storeDir, 0755); err != nil {
		fmt.Println("Error creating tree directory:", err)
	}
	fileObject, _ := os.Create(storeDir + "/" + string(hashString[2:]))
	defer fileObject.Close()

	writer := zlib.NewWriter(io.Writer(fileObject))
	writer.Write([]byte(content))
	writer.Close()
	return hash[:]
}
