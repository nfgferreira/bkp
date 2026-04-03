### bkp

__bkp__ compares the contents of path1 and path2 either in an exact or fast way. The former compares every byte of every file of the paths in a recursive way. The fast way takes only the modification time and file size to determine if the files are identical or not.

__bkp__ can:
- Create a list of files that are only in path 1
- Create a list of files that are only in path 2
- Create a list of files that exist in both paths but are different
- Create a list of files that exist in both paths but and are equal
- Accepts a time tolerance in fast comparison mode. If the files have the same size and their timestamps are in the tolerance, the files are considered equal.
- Take a configured action for the different files:
  - Copy files from path1 to path2
  - Copy files and directories that exist only in path1 to path2
  - Delete files and directories that exist only in path2

The command help provides the following explanation:
```
	Usage: bkp [OPTIONS]... path1 path2
	Compare the contents of path1 with path2.

	OPTIONS are:,
	-h --help      This help message
	-v --version   Program version
	-f --fast      Fast compare. Files are considered equal if their modification
	                 time and size match. If this option is not selected, 
	                 files are compared byte by byte.
	-1             Shows a list of the files in path1 only.
	-2             Shows a list of the files in path2 only.
	-d             Shows a list of different files that are in both paths.
	-e             Shows a list of equal files.
	-t=T --time=T  Time tolerance in secs when comparing files with option -f.
	                 The time T does not need to be an integer.
	-w=n           Option to copy files when differences are found. Nothing is
	                 done if this option is not provided or n is zero.
	                 n is the addition of the following values:
	                   1: To copy different files from path1 to path2
	                   2: To copy files and dirs that exist only in path1 to path2
	                   4: To delete files and dirs that exist only in path2
```
### Build
1. Install Go
2. go build bkp.go

### Version
0.1: First public release

### License
[GPLv3](https://www.gnu.org/licenses/gpl-3.0.en.html)

