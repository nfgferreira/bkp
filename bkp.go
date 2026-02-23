package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const VERSION = "0.1"

const COPY_DIFFERENT uint = 1
const COPY_IN_1_ONLY uint = 2
const DELETE_IN_2_ONLY uint = 4

var usage = [...]string{
	"Usage: bkp [OPTIONS]... path1 path2",
	"Compare the contents of path1 with path2.",
	"",
	"OPTIONS are:",
	"-h --help      This help message",
	"-v --version   Program version",
	"-f --fast      Fast compare. Files are considered equal if their modification",
	"                 time and size match. If this option is not selected, ",
	"                 files are compared byte by byte.",
	"-1             Shows a list of the files in path1 only.",
	"-2             Shows a list of the files in path2 only.",
	"-d             Shows a list of different files that are in both paths.",
	"-e             Shows a list of equal files.",
	"-t=T --time=T  Time tolerance in secs when comparing files with option -f.",
	"                 The time T does not need to be an integer.",
	"-w=n           Option to copy files when differences are found. Nothing is",
	"                 done if this option is not provided or n is zero.",
	"                 n is the addition of the following values:",
	"                   1: To copy different files from path1 to path2",
	"                   2: To copy files and dirs that exist only in path1 to path2",
	"                   4: To delete files and dirs that exist only in path2"}

type dirMembers map[string]bool

// Print version and terminate with code
func printVersion(executableName string) {
	fmt.Println(executableName, VERSION)
}

func (d1 *dirMembers) intersection(d2 *dirMembers) *dirMembers {
	result := make(dirMembers)
	if len(*d1) < len(*d2) {
		d1, d2 = d2, d1 // d1 always has less elements
	}
	for path := range *d1 {
		isDir, ok := (*d2)[path]
		if ok && isDir == (*d1)[path] {
			result[path] = isDir
		}
	}
	return &result
}

func (d1 *dirMembers) sub(d2 *dirMembers) *dirMembers {
	result := make(dirMembers)
	for path := range *d1 {
		result[path] = (*d1)[path]
	}
	for path := range *d2 {
		var isDir2 = (*d2)[path]
		isDir1, ok := (*d1)[path]
		if ok && isDir1 == isDir2 {
			delete(result, path)
		}
	}
	return &result
}

var help bool = false
var version bool = false
var fastCompare bool = false
var listIn1Only *bool
var listIn2Only *bool
var listDifferent *bool
var listEqual *bool
var writeConfiguration *uint
var timeTolerance float64 = 0
var copyIn1Only bool = false
var copyDifferent bool = false
var deleteIn2Only bool = false

func badUsage() {
	printUsage()
	os.Exit(1)
}

func printUsage() {
	for line := range usage {
		fmt.Println(usage[line])
	}
}

func main() {
	// Replace usage message using the command used by the user
	executableName := filepath.Base(os.Args[0])
	for i := range usage {
		usage[i] = strings.ReplaceAll(usage[i], "bkp", executableName)
	}

	flag.Usage = badUsage
	flag.BoolVar(&help, "h", false, "Print help message")
	flag.BoolVar(&help, "help", false, "Print help message")
	flag.BoolVar(&version, "v", false, "Print version")
	flag.BoolVar(&version, "version", false, "Print version")
	flag.BoolVar(&fastCompare, "f", false, "Use fast comparison, where time/date and size are enough to consider two files equal")
	flag.BoolVar(&fastCompare, "fast", false, "Use fast comparison, where time/date and size are enough to consider two files equal")
	listIn1Only = flag.Bool("1", false, "List files only in path 1.")
	listIn2Only = flag.Bool("2", false, "List files only in path 2.")
	listDifferent = flag.Bool("d", false, "List files which are different.")
	listEqual = flag.Bool("e", false, "List files which are the same.")
	flag.Float64Var(&timeTolerance, "t", 0, "Time tolerance when comparing file time and date if -f is enabled (default 0)")
	flag.Float64Var(&timeTolerance, "time", 0, "Time tolerance when comparing file time and date if -f is enabled (default 0)")
	writeConfiguration = flag.Uint("w", 0, "Actions to do when a difference is found (default nothing).")
	flag.Parse()
	if help {
		printUsage()
		os.Exit(0)
	}
	if version {
		printVersion(executableName)
		os.Exit(0)
	}
	if *writeConfiguration > 7 {
		fmt.Println("Invalid value for -w parameter. Valid values are 0 through 7.")
		os.Exit(1)
	}
	copyIn1Only = *writeConfiguration&COPY_IN_1_ONLY != 0
	copyDifferent = *writeConfiguration&COPY_DIFFERENT != 0
	deleteIn2Only = *writeConfiguration&DELETE_IN_2_ONLY != 0
	parameters := flag.Args()
	if len(parameters) != 2 {
		fmt.Println("Exactly two parameters were expected.")
		printUsage()
		os.Exit(1)
	}

	compare(parameters[0], parameters[1])

	// We delete the files in 2 only before copying the files in 1 only,
	// because we may have a directory in 1 and a file in 2 with the
	// same name or vice-versa.
	deleteFilesIn2Only()

	copyFilesIn1Only()

	// Now we sort the results and print them
	if *listIn1Only {
		if len(elementsIn1Only) != 0 {
			sort.Slice(elementsIn1Only, func(i, j int) bool {
				return elementsIn1Only[i].path1 < elementsIn1Only[j].path1
			})
			fmt.Println("")
			fmt.Println("--> Elements in path 1 only:")
			for _, pair := range elementsIn1Only {
				fmt.Println(pair.path1)
			}
		} else {
			fmt.Println("No elements found only in path 1.")
		}
	}

	if *listIn2Only {
		if len(elementsIn2Only) != 0 {
			sort.Slice(elementsIn2Only, func(i, j int) bool {
				return elementsIn2Only[i].path2 < elementsIn2Only[j].path2
			})
			fmt.Println("")
			fmt.Println("--> Elements in path 2 only:")
			for _, pair := range elementsIn2Only {
				fmt.Println(pair.path2)
			}
		} else {
			fmt.Println("No elements found only in path 2.")
		}
	}

	if len(elementsInError) != 0 {
		sort.Strings(elementsInError)
		fmt.Println("")
		fmt.Println("--> Errors found:")
		for _, path := range elementsInError {
			fmt.Println(path)
		}
	}

	fmt.Println("")
	if copyDifferent && numberOfDiffFileCopies == 0 {
		fmt.Println("No different files found.")
	} else if *listDifferent || numberOfDiffFileCopies > 0 {
		if len(elementsWhichAreDifferent) != 0 {
			sort.Slice(elementsWhichAreDifferent, func(i, j int) bool {
				return elementsWhichAreDifferent[i].path1 < elementsWhichAreDifferent[j].path1
			})
			if copyDifferent {
				fmt.Println("--> Pairs which were copied from source to destination:")
				for _, paths := range elementsWhichAreDifferent {
					fmt.Println(paths.path1 + " --> " + paths.path2)
				}
			} else {
				fmt.Println("--> Pairs which are different between path 1 and path 2:")
				for _, paths := range elementsWhichAreDifferent {
					fmt.Println(paths.path1 + ", " + paths.path2)
				}
			}
		} else {
			fmt.Println("No different files found.")
		}
		if numberOfDiffFileCopies > 0 {
			fmt.Printf("Number of different files copied: %d\n", numberOfDiffFileCopies)
		}
	}

	fmt.Println("")
	if *listEqual {
		if len(elementsWhichAreEqual) != 0 {
			sort.Slice(elementsWhichAreEqual, func(i, j int) bool {
				return elementsWhichAreEqual[i].path1 < elementsWhichAreEqual[j].path1
			})
			fmt.Println("--> Pairs which are the same between path 1 and path 2:")
			for _, paths := range elementsWhichAreEqual {
				fmt.Println(paths.path1 + ", " + paths.path2)
			}
		} else {
			fmt.Println("No equal files found.")
		}
	}

	os.Exit(0)
}

// Slices tahat will keep elements existing only in one of the paths
var elementsIn1Only []filePair           // Used for reporting purposes and to provide the files that must be copied from 1 to 2
var elementsIn2Only []filePair           // Used for reporting purposes and to provide the files in 2 that must be deleted
var numberOfDiffFileCopies int           // Used only for reporting purposes.
var elementsWhichAreEqual []filePair     // Used only for reporting purposes.
var elementsWhichAreDifferent []filePair // Used only for reporting purposes.
var elementsInError []string             // Errors are reported here so they are visible when the program stops.

// Correspondings semaphores
var sema1 = make(chan struct{}, 1)
var sema2 = make(chan struct{}, 1)
var semaCopyDiffFiles = make(chan struct{}, 1)
var semaEqual = make(chan struct{}, 1)
var semaDifferent = make(chan struct{}, 1)
var semaError = make(chan struct{}, 1)

// Add pair to elementsIn1Only
func addElementIn1Only(path1 string, path2 string) {
	sema1 <- struct{}{}
	defer func() { <-sema1 }()
	in1Only := filePair{path1: path1, path2: path2}
	elementsIn1Only = append(elementsIn1Only, in1Only)
}

// Add pair to elementsIn2Only
func addElementIn2Only(path1 string, path2 string) {
	sema2 <- struct{}{}
	defer func() { <-sema2 }()
	in2Only := filePair{path1: path1, path2: path2}
	elementsIn2Only = append(elementsIn2Only, in2Only)
}

// Add pair to elementsWhichAreEqual if they are equal.
// That is used only for reporting purposes.
func addEqualPair(path1 string, path2 string) {
	semaEqual <- struct{}{}
	defer func() { <-semaEqual }()
	files2Compare := filePair{path1: path1, path2: path2}
	elementsWhichAreEqual = append(elementsWhichAreEqual, files2Compare)
}

func addDifferentPair(path1 string, path2 string) {
	semaDifferent <- struct{}{}
	defer func() { <-semaDifferent }()
	files2Compare := filePair{path1: path1, path2: path2}
	elementsWhichAreDifferent = append(elementsWhichAreDifferent, files2Compare)
}

func addElementInError(path string) {
	semaError <- struct{}{}
	defer func() { <-semaError }()
	elementsInError = append(elementsInError, path)
}

// WaitGroup to synchronize the completion of all comparisons
var waitDone sync.WaitGroup

type filePair struct {
	path1 string
	path2 string
}

// Here we pick 2 paths and trigger a recursive traverse.
func compare(dir1 string, dir2 string) {

	paralelism := MaxParallelism()

	// Create channel where all the files go
	fileChannel := make(chan filePair, paralelism*10)

	// Trigger instances to read fileChannel and compare the files
	for range paralelism {
		go compareFiles(fileChannel)
	}

	// Trigger recursive trasversal that feeds fileChannel
	err := traverse(dir1, dir2, fileChannel)

	close(fileChannel) // Close the channel
	waitDone.Wait()    // Wait until all comparisons are done

	if err != nil {
		fmt.Printf("Error during traversal: %v\n", err)
	} else if len(fileChannel) != 0 {
		fmt.Println("For some reason not all comparisons finished.")
	} else {
		fmt.Println("Traversal completed succesfully.")
	}
}

// Traverse the directory trees and add files to fileChannel as they are found.
func traverse(dir1 string, dir2 string, fileChannel chan<- filePair) error {
	// Calculate the children of dir1
	dirEntry1, err1 := os.ReadDir(dir1)
	if err1 != nil {
		addElementInError(dir1 + ": " + err1.Error())
		return err1
	}

	// Calculate the children of dir2
	dirEntry2, err2 := os.ReadDir(dir2)
	if err2 != nil {
		addElementInError(dir2 + ": " + err2.Error())
		return err2
	}

	// Create set of d1 members
	d1 := make(dirMembers)
	for _, entry := range dirEntry1 {
		d1[entry.Name()] = entry.IsDir()
	}

	// Create set of d2 members
	d2 := make(dirMembers)
	for _, entry := range dirEntry2 {
		d2[entry.Name()] = entry.IsDir()
	}

	// Create sets with common files and files only in one of the directories
	commonFiles := d1.intersection(&d2)
	filesInD1Only := d1.sub(&d2)
	filesInD2Only := d2.sub(&d1)

	// Store the elements in d1 only in elementsIn1Only
	if len(*filesInD1Only) != 0 {
		for path1 := range *filesInD1Only {
			if (*filesInD1Only)[path1] {
				path1 += "/"
			}
			addElementIn1Only(dir1+"/"+path1, dir2+"/"+path1)
		}
	}

	// Register the elements in d2 only:
	if len(*filesInD2Only) != 0 {
		for path2 := range *filesInD2Only {
			if (*filesInD2Only)[path2] {
				path2 += "/"
			}
			addElementIn2Only(dir1+"/"+path2, dir2+"/"+path2)
		}
	}

	// Traverse the common members
	if len(*commonFiles) != 0 {
		for path := range *commonFiles {
			if (*commonFiles)[path] {
				traverse(dir1+"/"+path, dir2+"/"+path, fileChannel)
			} else {
				var cmp filePair
				cmp.path1 = dir1 + "/" + path
				cmp.path2 = dir2 + "/" + path
				waitDone.Add(1)
				fileChannel <- cmp
			}
		}
	}

	return nil
}

func compareFiles(fileChannel <-chan filePair) {
	for {
		cmp, ok := <-fileChannel
		if !ok {
			return // Channel is closed and empty: do nothing
		}

		var file1Info, err1 = os.Stat(cmp.path1)
		var file2Info, err2 = os.Stat(cmp.path2)

		if err1 != nil || err2 != nil {
			if err1 != nil {
				addElementInError(cmp.path1 + ": " + err1.Error())
			}
			if err2 != nil {
				addElementInError(cmp.path2 + ": " + err2.Error())
			}
		} else if file1Info.Size() != file2Info.Size() {
			copyDifferentFiles(cmp.path1, cmp.path2)
			addDifferentPair(cmp.path1, cmp.path2)
		} else if fastCompare {
			file1Time := file1Info.ModTime()
			file1MinTime := file1Time.Add(time.Duration(-timeTolerance * float64(time.Second)))
			file1MaxTime := file1Time.Add(time.Duration(timeTolerance * float64(time.Second)))
			if file2Info.ModTime().Before(file1MinTime) || file2Info.ModTime().After(file1MaxTime) {
				copyDifferentFiles(cmp.path1, cmp.path2)
				addDifferentPair(cmp.path1, cmp.path2)
			} else {
				addEqualPair(cmp.path1, cmp.path2)
			}
		} else {
			f1, err1 := os.Open(cmp.path1)
			if err1 != nil {
				addElementInError(cmp.path1 + ": " + err1.Error())
				f1.Close()
				continue
			}

			f2, err2 := os.Open(cmp.path2)
			if err2 != nil {
				addElementInError(cmp.path2 + ": " + err2.Error())
				f2.Close()
				continue
			}

			const bufferSize = 16 * 4096
			buf1 := make([]byte, bufferSize)
			buf2 := make([]byte, bufferSize)

			for {
				n1, err1 := f1.Read(buf1)
				n2, err2 := f2.Read(buf2)

				if err1 != nil && err1 != io.EOF {
					addElementInError(cmp.path1 + ": " + err1.Error())
					break
				}
				if err2 != nil && err2 != io.EOF {
					addElementInError(cmp.path2 + ": " + err2.Error())
					break
				}

				if n1 != n2 || !bytes.Equal(buf1[:n1], buf2[:n2]) {
					copyDifferentFiles(cmp.path1, cmp.path2)
					addDifferentPair(cmp.path1, cmp.path2)
					break
				}

				if err1 == io.EOF && err2 == io.EOF {
					addEqualPair(cmp.path1, cmp.path2)
					break
				}
			}
			f1.Close()
			f2.Close()
		}
		waitDone.Done()
	}
}

func copyDifferentFiles(path1, path2 string) {
	if copyDifferent {
		semaCopyDiffFiles <- struct{}{}
		numberOfDiffFileCopies++
		<-semaCopyDiffFiles
		ok := copyFile(path1, path2)
		if ok != nil {
			addElementInError(path1 + "->" + path2 + ": " + ok.Error())
		}
	}
}

func copyFile(path1, path2 string) error {
	input, err := os.Open(path1)
	if err != nil {
		return err
	}
	defer input.Close()

	path1Info, err := input.Stat()
	if err != nil {
		return err
	}

	output, err := os.Create(path2)
	if err != nil {
		return err
	}
	defer output.Close()

	_, err = io.Copy(output, input)
	if err != nil {
		return err
	}

	err = output.Sync()
	if err != nil {
		return err
	}

	err = os.Chmod(path2, path1Info.Mode().Perm())
	if err != nil {
		return err
	}

	modTime := path1Info.ModTime()
	accessTime := modTime // A common practice is to use the modification time for the access time if the original access time isn't readily available.

	err = os.Chtimes(path2, accessTime, modTime)
	if err != nil {
		return err
	}

	return nil
}

// Copy the files that are in 1 only to 2, if the corresponding option is enabled.
// Do not copy directories, even if they are in 1 only,
// because they have already been created during traversal.
func copyFilesIn1Only() {
	if copyIn1Only {
		for _, pair := range elementsIn1Only {
			fullPath1 := pair.path1
			fullPath2 := pair.path2

			var ok error
			if pathIsDir(pair.path1) {
				ok = copyDir(fullPath1, fullPath2)
			} else {
				ok = copyFile(fullPath1, fullPath2)
			}
			if ok != nil {
				addElementInError(fullPath1 + "->" + fullPath2 + ": " + ok.Error())
			}
		}
	}
}

// CopyDir recursively copies a directory tree.
// We know both paths are directories, but we check anyways.
func copyDir(src string, dst string) (err error) {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !si.IsDir() {
		return fmt.Errorf("source %s is not a directory", src)
	}

	_, err = os.Stat(dst)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err == nil {
		return fmt.Errorf("destination %s already exists", dst)
	}

	err = os.MkdirAll(dst, si.Mode())
	if err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			err = copyDir(srcPath, dstPath)
			if err != nil {
				return err
			}
		} else {
			// Skip symlinks for simplicity in this example
			if entry.Type()&fs.ModeSymlink != 0 {
				continue
			}
			err = copyFile(srcPath, dstPath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func deleteFilesIn2Only() {
	if deleteIn2Only {
		for _, pair := range elementsIn2Only {
			fullPath2 := pair.path2
			var ok error
			if pathIsDir(pair.path2) {
				ok = os.RemoveAll(pair.path2)
			} else {
				ok = os.Remove(fullPath2)
			}
			if ok != nil {
				addElementInError("Remove " + fullPath2 + ": " + ok.Error())
			}
		}
	}
}

func pathIsDir(path string) bool {
	return path[len(path)-1] == '/'
}

// from https://stackoverflow.com/questions/13234749/golang-how-to-verify-number-of-processors-on-which-a-go-program-is-running
func MaxParallelism() int {
	maxProcs := runtime.GOMAXPROCS(0)
	numCPU := runtime.NumCPU()
	if maxProcs < numCPU {
		return maxProcs
	}
	return numCPU
}
