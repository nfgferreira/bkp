package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
)

type dirMembers map[string]struct{}

func (d1 *dirMembers) intersection(d2 *dirMembers) *dirMembers {
	result := make(dirMembers)
	if len(*d1) < len(*d2) {
		d1, d2 = d2, d1 // d1 always has less elements
	}
	for path := range *d1 {
		_, ok := (*d2)[path]
		if ok {
			result[path] = struct{}{}
		}
	}
	return &result
}

func (d1 *dirMembers) sub(d2 *dirMembers) *dirMembers {
	result := make(dirMembers)
	for path := range *d1 {
		result[path] = struct{}{}
	}
	for path := range *d2 {
		delete(result, path)
	}
	return &result
}

func main() {
	var simulate = flag.Bool("s", true, "Simulate backup")
	var fast = flag.Bool("f", true, "Use fast comparison")
	var timeTolerance = flag.Int("t", 0, "Time tolerance when comparing file time and date (default 0)")
	flag.Parse()
	fmt.Printf("Simulate = %t\n", *simulate)
	fmt.Printf("Fast = %t\n", *fast)
	fmt.Printf("Time tolerance = %d\n", *timeTolerance)
	parameters := flag.Args()
	fmt.Println(parameters)
	if len(parameters) != 2 {
		fmt.Println("Exactly two parameters were expected.")
		os.Exit(1)
	}

	dirCompare(parameters[0], parameters[1])
	os.Exit(0)
}

func dirCompare(dir1 string, dir2 string) {
	fmt.Printf("Comparing %v and %v\n", dir1, dir2)

	// Calculate the children of dir1
	dirEntry1, err1 := os.ReadDir(dir1)
	if err1 != nil {
		fmt.Printf("%v.\n", err1)
		return
	}

	// Calculate the children of dir2
	dirEntry2, err2 := os.ReadDir(dir2)
	if err2 != nil {
		fmt.Printf("%v.\n", err2)
		return
	}

	// Create set of d1 members
	d1 := make(dirMembers)
	for _, entry := range dirEntry1 {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		d1[name] = struct{}{}
	}

	// Create set of d2 members
	d2 := make(dirMembers)
	for _, entry := range dirEntry2 {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		d2[name] = struct{}{}
	}

	// Create sets with common files and files only in one of the directories
	commonFiles := d1.intersection(&d2)
	filesInD1Only := d1.sub(&d2)
	filesInD2Only := d2.sub(&d1)

	// Print the files only in d1:
	if len(*filesInD1Only) != 0 {
		fmt.Println("Files only in source", dir1)
		for path1 := range *filesInD1Only {
			fmt.Println(path1)
		}
	}

	// Print the files only in d2:
	if len(*filesInD2Only) != 0 {
		fmt.Println("Files only in destination", dir2)
		for path2 := range *filesInD2Only {
			fmt.Println(path2)
		}
	}

	// Print the common files
	if len(*commonFiles) != 0 {
		fmt.Println("Common files")
		for path := range *commonFiles {
			fmt.Println(path)
		}
	}

	// Now compare the files
	compareFilesIn(commonFiles)

	fmt.Println(MaxParallelism())

}

// We want to compare files here and be fast.
// The processing will be shared amongst the maximum number of cores.
func compareFilesIn(commonFiles *dirMembers) {
	nCores := MaxParallelism() // Number of available cores.

	// We do not count the core which is running this process
	if nCores > 1 {
		nCores--
	}

	// Now we enumerate the files to be allocated to each core.
	// Reading maps in Go is non-determinitic, so we create a slice to list all the
	// elements to be compared.
	fileList := make([]string, 0, len(*commonFiles))
	for path := range *commonFiles {
		fileList = append(fileList, path)
	}

	// Now we calculate the numer of files to each core. We try to balance the load as much as we can.
	nFiles := len(fileList)
	filesPerCore := nFiles / nCores
	extraFiles := nFiles % nCores

	fmt.Printf("Number of cores: %d, files per core: %d, extra files: %d\n", nCores, filesPerCore, extraFiles)
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
