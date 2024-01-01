package root

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"sync"

	"github.com/spf13/cobra"
)

var (
	outputFile string
	recursive  bool
)

const maxOpenFileDescriptors = 1000

type Result struct {
	channel      map[string]chan string
	searchResult map[string][]string
	mu           sync.Mutex
}

var grepCmd = &cobra.Command{
	Use:   "grep",
	Short: "grep is a CLI tool to search for a string in a file",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 2 {
			fmt.Println("Please provide a string and a file-name to search in")
			return
		}

		if !recursive {
			str := args[0]
			file := args[1]
			fmt.Println("Searching for string: in file: ", file)

			grepString(file, str)
		} else {
			str := args[0]
			dir := args[1]
			fmt.Println("Searching for string: in directory: ", dir)
			files := []string{}
			recursiveGrepString(dir, str, &files)
			var wg sync.WaitGroup

			result := Result{
				channel:      make(map[string]chan string),
				searchResult: make(map[string][]string),
			}
			resultSyncChannel := make(chan string)
			quit := make(chan int)
			go result.writeResultToStdout(resultSyncChannel, quit)
			fileDescriptorBuffer := make(chan int, maxOpenFileDescriptors)
			for _, file := range files {
				fileBuffCh := make(chan string)
				result.mu.Lock()
				result.channel[file] = fileBuffCh
				result.searchResult[file] = []string{}
				result.mu.Unlock()
				go readFileByLine(file, fileBuffCh, fileDescriptorBuffer)
				wg.Add(1)
				go result.gatherResult(str, file, resultSyncChannel, &wg)
			}
			wg.Wait()
			quit <- 1
			close(resultSyncChannel)
			close(quit)
		}
	},
}

func (r *Result) writeResultToStdout(resultSyncChannel chan string, quit chan int) {
	for {
		select {
		case fileName := <-resultSyncChannel:
			r.mu.Lock()
			for _, op := range r.searchResult[fileName] {
				fmt.Println("file: ", fileName, " string : ", op)
			}
			r.mu.Unlock()
		case <-quit:
			return
		}
	}
}

func (result *Result) gatherResult(searchQuery string, fileName string, resultSyncChannel chan string, wg *sync.WaitGroup) {
	defer wg.Done()
	result.mu.Lock()
	fileReadChannel := result.channel[fileName]
	result.mu.Unlock()
	for text := range fileReadChannel {
		searchResult := strings.Contains(text, searchQuery)
		if searchResult {
			result.mu.Lock()
			result.searchResult[fileName] = append(result.searchResult[fileName], fmt.Sprintf("%s", text))
			result.mu.Unlock()
		}
	}
	// Send message to result sync channel denoting current file search is complete
	resultSyncChannel <- fileName
}

func readFileByLine(file string, fileBuffCh chan string, fileDescriptorBuffer chan int) {
	fileDescriptorBuffer <- 1

	file_, err := os.Open(file)
	if err != nil {
		fmt.Println("Error opening file")
		return
	}
	defer func() {
		file_.Close()
		close(fileBuffCh)
		<-fileDescriptorBuffer
	}()

	scanner := bufio.NewScanner(file_)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		fileBuffCh <- line
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file")
		return
	}

}

func recursiveGrepString(dir string, str string, files *[]string) {
	currentDir, err := os.Open(dir)
	if err != nil {
		fmt.Println("Error getting directory contents")
		return
	}
	defer currentDir.Close()
	entries, err := currentDir.Readdir(-1)

	if err != nil {
		fmt.Println("Error reading directory contents")
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			recursiveGrepString(dir+"/"+entry.Name(), str, files)
		} else {
			*files = append(*files, dir+"/"+entry.Name())
		}
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&outputFile, "output", "o", "", "Output file to write to")
	rootCmd.PersistentFlags().BoolVarP(&recursive, "recursive", "r", false, "Search recursively in a directory")
	rootCmd.AddCommand(grepCmd)
}

func grepString(file string, str string) {
	fileInfo, err := os.Stat(file)
	if err != nil {
		fmt.Println("Error opening file")
		return
	}
	if fileInfo.IsDir() {
		fmt.Println("Error: ", file, " is a directory")
		return
	}

	file_, err := os.Open(file)
	if err != nil {
		fmt.Println("Error opening file")
		return
	}
	defer file_.Close()
	var outputFile_ *os.File

	if outputFile != "" {
		_, err := os.Create(outputFile)
		if err != nil {
			fmt.Println("Error creating output file")
			return
		}
		outputFile_, err = os.OpenFile(outputFile, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Println("Error opening output file")
			return
		}
	}

	scanner := bufio.NewScanner(file_)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, str) {
			fmt.Println("String found in file ", file, " : ", str)
			if outputFile != "" {
				outputFile_.WriteString(line)
				outputFile_.WriteString("\n")
			}
		}
	}
}
