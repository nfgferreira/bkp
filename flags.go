package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"sync"
)

type dirMembers map[string]bool

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

var fastCompare *bool
var listIn1Only *bool
var listIn2Only *bool
var listDifferent *bool
var listEqual *bool
var writeConfiguration *uint
var timeTolerance *int

func main() {
	fastCompare = flag.Bool("f", false, "Use fast comparison, where time/date and size are enough to consider two files equal")
	listIn1Only = flag.Bool("1", false, "List files only in path 1.")
	listIn2Only = flag.Bool("2", false, "List files only in path 2.")
	listDifferent = flag.Bool("d", false, "List files which are different.")
	listEqual = flag.Bool("s", false, "List files which are the same.")
	timeTolerance = flag.Int("t", 0, "Time tolerance when comparing file time and date if -f is enabled (default 0)")
	writeConfiguration = flag.Uint("w", 0, "Write: 0(disabled), 1(!= -> 2), 2(1->2), 3(1, != -> 2) (default 0)")
	flag.Parse()
	if *writeConfiguration > 3 {
		fmt.Println("Invalid value for -w parameter. Valid values are 0, 1, 2 and 3.")
		os.Exit(1)
	}
	parameters := flag.Args()
	if len(parameters) != 2 {
		fmt.Println("Exactly two parameters were expected.")
		os.Exit(1)
	}

	compare(parameters[0], parameters[1])

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
	if (*writeConfiguration == 1 || *writeConfiguration == 3) && numberOfDiffFileCopies == 0 {
		fmt.Println("No different files found.")
	} else if *listDifferent || numberOfDiffFileCopies > 0 {
		if len(elementsWhichAreDifferent) != 0 {
			sort.Slice(elementsWhichAreDifferent, func(i, j int) bool {
				return elementsWhichAreDifferent[i].path1 < elementsWhichAreDifferent[j].path1
			})
			fmt.Println("--> Pairs which are different between path 1 and path 2:")
			for _, paths := range elementsWhichAreDifferent {
				fmt.Println(paths.path1 + ", " + paths.path2)
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
var elementsIn1Only []files2Compare
var elementsIn2Only []files2Compare
var numberOfDiffFileCopies int
var elementsWhichAreEqual []files2Compare
var elementsWhichAreDifferent []files2Compare
var elementsInError []string

// Correspondings semaphores
var sema1 = make(chan struct{}, 1)
var sema2 = make(chan struct{}, 1)
var semaCopyDiffFiles = make(chan struct{}, 1)
var semaEqual = make(chan struct{}, 1)
var semaDifferent = make(chan struct{}, 1)
var semaError = make(chan struct{}, 1)

func addElementIn1Only(path1 string, path2 string) {
	sema1 <- struct{}{}
	defer func() { <-sema1 }()
	in1Only := files2Compare{path1: path1, path2: path2}
	elementsIn1Only = append(elementsIn1Only, in1Only)
}

func addElementIn2Only(path1 string, path2 string) {
	sema2 <- struct{}{}
	defer func() { <-sema2 }()
	in2Only := files2Compare{path1: path1, path2: path2}
	elementsIn2Only = append(elementsIn2Only, in2Only)
}

func addEqualPair(path1 string, path2 string) {
	semaEqual <- struct{}{}
	defer func() { <-semaEqual }()
	files2Compare := files2Compare{path1: path1, path2: path2}
	elementsWhichAreEqual = append(elementsWhichAreEqual, files2Compare)
}

func addDifferentPair(path1 string, path2 string) {
	semaDifferent <- struct{}{}
	defer func() { <-semaDifferent }()
	files2Compare := files2Compare{path1: path1, path2: path2}
	elementsWhichAreDifferent = append(elementsWhichAreDifferent, files2Compare)
}

func addElementInError(path string) {
	semaError <- struct{}{}
	defer func() { <-semaError }()
	elementsInError = append(elementsInError, path)
}

// WaitGroup to synchronize the completion of all comparisons
var waitDone sync.WaitGroup

type files2Compare struct {
	path1 string
	path2 string
}

// Here we pick 2 paths and trigger a recursive traverse.
func compare(dir1 string, dir2 string) {

	paralelism := MaxParallelism()

	// Create channel where all the files go
	fileChannel := make(chan files2Compare, paralelism*10)

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
func traverse(dir1 string, dir2 string, fileChannel chan<- files2Compare) error {
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

	// Register the elements in d1 only:
	if len(*filesInD1Only) != 0 {
		for path1 := range *filesInD1Only {
			if (*filesInD1Only)[path1] {
				path1 += "/"
			}
			addElementIn1Only(dir1+"/"+path1, dir2+"/"+path1)
		}
	}

	// Register the elements in d1 only:
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
				var cmp files2Compare
				cmp.path1 = dir1 + "/" + path
				cmp.path2 = dir2 + "/" + path
				waitDone.Add(1)
				fileChannel <- cmp
			}
		}
	}

	return nil
}

func compareFiles(fileChannel <-chan files2Compare) {
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
		} else if *fastCompare {
			if file1Info.ModTime() != file2Info.ModTime() {
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
	if *writeConfiguration == 1 || *writeConfiguration == 3 {
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

func copyFilesIn1Only() {
	if *writeConfiguration == 1 || *writeConfiguration == 3 {
		for _, pair := range elementsIn1Only {
			fullPath1 := pair.path1
			fullPath2 := pair.path2
			ok := copyFile(fullPath1, fullPath2)
			if ok != nil {
				addElementInError(fullPath1 + "->" + fullPath2 + ": " + ok.Error())
			}
		}
	}
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
