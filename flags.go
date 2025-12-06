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
var timeTolerance *int

func main() {
	fastCompare = flag.Bool("f", false, "Use fast comparison, where time/date and size are enough to consider two files equal")
	timeTolerance = flag.Int("t", 0, "Time tolerance when comparing file time and date if -f is enabled (default 0)")
	flag.Parse()
	fmt.Printf("Fast = %t\n", *fastCompare)
	fmt.Printf("Time tolerance = %d\n", *timeTolerance)
	parameters := flag.Args()
	fmt.Println(parameters)
	if len(parameters) != 2 {
		fmt.Println("Exactly two parameters were expected.")
		os.Exit(1)
	}

	compare(parameters[0], parameters[1])

	// Now we sort the results and print them
	if len(elementsIn1Only) != 0 {
		sort.Strings(elementsIn1Only)
		fmt.Println("Elements in path 1 only:")
		for _, path := range elementsIn1Only {
			fmt.Println(path)
		}
	}

	if len(elementsIn2Only) != 0 {
		sort.Strings(elementsIn2Only)
		fmt.Println("Elements in path 2 only:")
		for _, path := range elementsIn2Only {
			fmt.Println(path)
		}
	}

	if len(elementsInError) != 0 {
		sort.Strings(elementsInError)
		fmt.Println("Elements that couldn't be opened and the errors returned:")
		for _, path := range elementsInError {
			fmt.Println(path)
		}
	}

	if len(elementsWhichAreDifferent) != 0 {
		sort.Slice(elementsWhichAreDifferent, func(i, j int) bool {
			return elementsWhichAreDifferent[i].path1 < elementsWhichAreDifferent[j].path1
		})
		fmt.Println("Pairs which are different between path 1 and path 2:")
		for _, paths := range elementsWhichAreDifferent {
			fmt.Println(paths.path1 + ", " + paths.path2)
		}
	}

	if len(elementsWhichAreEqual) != 0 {
		sort.Slice(elementsWhichAreEqual, func(i, j int) bool {
			return elementsWhichAreEqual[i].path1 < elementsWhichAreEqual[j].path1
		})
		fmt.Println("Pairs which are the same between path 1 and path 2:")
		for _, paths := range elementsWhichAreEqual {
			fmt.Println(paths.path1 + ", " + paths.path2)
		}
	}

	os.Exit(0)
}

// Slices tahat will keep elements existing only in one of the paths
var elementsIn1Only []string
var elementsIn2Only []string
var elementsWhichAreEqual []files2Compare
var elementsWhichAreDifferent []files2Compare
var elementsInError []string

// Correspondings semaphores
var sema1 = make(chan struct{}, 1)
var sema2 = make(chan struct{}, 1)
var semaEqual = make(chan struct{}, 1)
var semaDifferent = make(chan struct{}, 1)
var semaError = make(chan struct{}, 1)

func addElementIn1Only(path string) {
	sema1 <- struct{}{}
	defer func() { <-sema1 }()
	elementsIn1Only = append(elementsIn1Only, path)
}

func addElementIn2Only(path string) {
	sema2 <- struct{}{}
	defer func() { <-sema2 }()
	elementsIn2Only = append(elementsIn2Only, path)
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

	if len(fileChannel) != 0 {
		fmt.Print("WEIRD\n")
	} else {
		fmt.Print("FIM OK\n")
	}

	if err != nil {
		fmt.Printf("Error during traversal: %v\n", err)
	} else {
		fmt.Println("Traversal completed.")
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
			addElementIn1Only(dir1 + "/" + path1)
		}
	}

	// Register the elements in d1 only:
	if len(*filesInD2Only) != 0 {
		for path2 := range *filesInD2Only {
			if (*filesInD2Only)[path2] {
				path2 += "/"
			}
			addElementIn2Only(dir2 + "/" + path2)
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
			addDifferentPair(cmp.path1, cmp.path2)
		} else if *fastCompare {
			if file1Info.ModTime() != file2Info.ModTime() {
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

			const bufferSize = 4096
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

// from https://stackoverflow.com/questions/13234749/golang-how-to-verify-number-of-processors-on-which-a-go-program-is-running
func MaxParallelism() int {
	maxProcs := runtime.GOMAXPROCS(0)
	numCPU := runtime.NumCPU()
	if maxProcs < numCPU {
		return maxProcs
	}
	return numCPU
}
