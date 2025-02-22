package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type ObjectType int

const (
	OBJ_COMMIT    ObjectType = 1
	OBJ_TREE      ObjectType = 2
	OBJ_BLOB                 = 3
	OBJ_TAG                  = 4
	OBJ_OFS_DELTA            = 6
	OBJ_REF_DELTA            = 7
)

func (objType ObjectType) String() string {
	switch objType {
	case OBJ_TREE:
		return "tree"
	case OBJ_COMMIT:
		return "commit"
	case OBJ_BLOB:
		return "blob"
	case OBJ_TAG:
		return "tag"
	default:
		return "unknown"
	}
}

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
		gitRepo := os.Args[2]
		dir := os.Args[3]

		// create the directory
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			panic(err)
		}
		// change to the new directory created to run all the other file creations
		err = os.Chdir(dir)
		if err != nil {
			panic(err)
		}

		res, err := http.Get(fmt.Sprintf("%s/info/refs?service=git-upload-pack", gitRepo))
		if err != nil {
			fmt.Println("could not make request", err)
		}
		defer res.Body.Close()

		fmt.Println("Response status:", res.Status)

		refs := make(map[string]string)

		scanner := bufio.NewScanner(res.Body)

		// Response body
		/* 001e# service=git-upload-pack
		0000015523f0bc3b5c7c3108e41c448f01a3db31e7064bbb HEAD[null]multi_ack thin-pack side-band side-band-64k ofs-delta shallow deepen-since deepen-not deepen-relative no-progress include-tag multi_ack_detailed allow-tip-sha1-in-want allow-reachable-sha1-in-want no-done symref=HEAD:refs/heads/master filter object-format=sha1 agent=git/github-d6c9584635a2
		003f23f0bc3b5c7c3108e41c448f01a3db31e7064bbb refs/heads/master
		0000 */

		// Process each line of the response
		for scanner.Scan() {
			line := scanner.Bytes()

			// Skip lines that start with '#'
			if len(line) > 4 && string(line[4:]) != "" && !bytes.HasPrefix(line[4:], []byte("#")) {
				// Split the line by null byte
				parts := bytes.Split(line[4:], []byte{0x00})
				if len(parts) > 0 {
					chunk2 := parts[0]

					// Check if the string ends with "HEAD", then remove the first 4 characters
					if len(chunk2) > 4 && bytes.HasSuffix(chunk2, []byte("HEAD")) {
						chunk2 = chunk2[4:]
					}

					// Split by space to form the chunk array
					chunk := bytes.Split(chunk2, []byte(" "))
					if len(chunk) >= 2 {
						// Decode chunk[0] and chunk[1] and store them in refs map
						refs[string(chunk[1])] = string(chunk[0])
					}
				}
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Println("Error scanning response body:", err)
		}

		// Print the resulting map (refs)
		for key, value := range refs {
			fmt.Printf("%s: %s\n", key, value)
		}

		buff := new(bytes.Buffer)
		fmt.Fprintf(buff, "0032want %s\n00000009done\n", refs["HEAD"])
		//	buffer := bytes.NewBufferString(fmt.Sprintf("0032want %s\n00000009done\n", refs["HEAD"]))
		packResponse, packReqErr := http.Post(fmt.Sprintf("%s/git-upload-pack", gitRepo), "application/x-git-upload-pack-request", buff)
		if packReqErr != nil {
			fmt.Println("error getting ref packs")
		}
		defer packResponse.Body.Close()

		response := bytes.Buffer{}
		io.Copy(&response, packResponse.Body)
		// start processing the header
		// cut out till you get till after PACK
		offset := bytes.Index(response.Bytes(), []byte("PACK")) + 4

		packFile := response.Bytes()
		// remove the check sum at the end of the file/ bytes not needed for processing
		packFile = packFile[:len(packFile)-20]

		// get the verions
		version := binary.BigEndian.Uint32(packFile[offset : offset+4])
		offset = offset + 4
		// get the number of objects in the packfile
		numOfObjects := binary.BigEndian.Uint32(packFile[offset : offset+4])
		// increase offset for the processed bytes
		offset = offset + 4

		fmt.Println(numOfObjects)
		fmt.Println(len(packFile))

		// start going through the objects
		for range numOfObjects {
			// get park object header
			parkSize, objectType, used, err := parseObjectHeader(packFile[offset:])
			if err != nil {
				fmt.Println("There is a bad object header")
			}
			offset += used
			if objectType == OBJ_TREE || objectType == OBJ_COMMIT || objectType == OBJ_BLOB || objectType == OBJ_TAG {
				data, read, err := readObject(packFile[offset:])
				offset += read
				if err != nil {
					fmt.Println("An error occurred while reading object ", err)
				}
				if int(read) != len(data) {
					fmt.Println("there is an error with the data length")
				}
				_ = writeObjectWithType(data, objectType)
			}
			if objectType == OBJ_REF_DELTA {
				baseObjHash := hex.EncodeToString(packFile[offset:20])
				offset += 20
				content, used, err := readObject(packFile[offset:])
				if err != nil {
					fmt.Println("There is an error reading the obj delta content")
				}
				offset += used
				sourceSize, read := parseDeltaSize(packFile[offset:])
				offset += read
				targetSize, read := parseDeltaSize(packFile[offset:])

			}
			fmt.Println("this is the data we just got ", parkSize, objectType)
			fmt.Printf("Thius is the size of the buffer processed %d\n", offset)
		}
		fmt.Println(version)

		//fmt.Println(string(response.Bytes()))

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

func writeObjectWithType(content []byte, objectType ObjectType) []byte {
	blob := bytes.Buffer{}
	fmt.Fprintf(&blob, "%s %d", objectType, len(content))
	blob.WriteByte(0)
	blob.Write(content)
	// Write to disk
	hash := writeObject(blob.Bytes())
	return hash
}

func parseObjectHeader(data []byte) (size uint64, objectType ObjectType, used int, err error) {
	byteData := data[used]
	used++
	objectType = ObjectType((byteData >> 4) & 0x7)
	size = uint64(byteData & 0xF)
	shift := 4
	for byteData&0x80 != 0 {
		if len(data) <= used || 64 <= shift {
			return 0, ObjectType(0), 0, errors.New("bad object header")
		}
		byteData = data[used]
		used++
		size += uint64(byteData&0x7F) << shift
		shift += 7
	}
	return size, objectType, used, nil
}

func parseDeltaSize(packFile []byte) (int, int) {
	size := packFile[0] & 0b01111111
	index, off := 1, 7

	for packFile[index-1]&0b10000000 > 0 { // Check if MSB is set
		size = size | (packFile[index]&0b01111111)<<off
		off += 7
		index += 1
	}

	// this index is the same as the used bytes

	return int(size), index
}

func readObject(packFile []byte) (data []byte, used int, err error) {
	reader := bytes.NewReader(packFile)
	r, err := zlib.NewReader(reader)
	if err != nil {
		return nil, 0, err
	}
	defer r.Close()

	decompData, err := io.ReadAll(r)
	if err != nil {
		return nil, 0, err
	}

	used = int(reader.Size()) - int(reader.Len())

	return decompData, used, nil
}
