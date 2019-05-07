package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func main() {
	destDir := flag.String("dest", "", "destination directory")
	user := flag.String("user", "", "user name")
	repo := flag.String("repo", "", "repository name")
	pkg := flag.String("pkg", "", "package name")
	version := flag.String("version", "", "package version")
	timeout := flag.Duration("timeout", 5*time.Minute, "http client timeout")
	flag.Parse()

	buildURL, err := getBuildURL(*user, *repo, *pkg, *version)
	if err != nil {
		log.Fatal(err)
	}
	fileURLs, err := getFileURLs(buildURL)
	if err != nil {
		log.Fatal(err)
	}
	err = downloadFiles(fileURLs, *timeout, *destDir)
	if err != nil {
		log.Fatal(err)
	}
}

func getBuildURL(user, repo, pkg, version string) (*url.URL, error) {
	packagesURL, err := url.Parse(fmt.Sprintf("https://launchpad.net/~%s/+archive/ubuntu/%s/+packages?nocache_dummy=%d", user, repo, time.Now().Unix()))
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocument(packagesURL.String())
	if err != nil {
		return nil, err
	}
	// Get latest build URL.
	var buildURL *url.URL
	doc.Find("a.expander").Each(func(i int, s *goquery.Selection) {
		if buildURL != nil {
			return
		}
		words := strings.Split(strings.TrimSpace(s.Text()), " - ")
		if len(words) != 2 || words[0] != pkg {
			return
		}
		if version != "" && words[1] != version {
			return
		}
		href, exists := s.Attr("href")
		if !exists {
			return
		}
		hrefURL, err := url.Parse(href)
		if err != nil {
			log.Println(err)
		}
		buildURL = packagesURL.ResolveReference(hrefURL)
	})
	if buildURL == nil {
		return nil, errors.New("build not found for package")
	}
	return buildURL, nil
}

func getFileURLs(buildURL *url.URL) ([]string, error) {
	doc, err := goquery.NewDocument(buildURL.String())
	if err != nil {
		return nil, err
	}
	var fileURLs []string
	doc.Find("li.package a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}
		fileURLs = append(fileURLs, href)
	})
	return fileURLs, nil
}

func downloadFiles(fileURLs []string, timeout time.Duration, destDir string) error {
	if destDir == "" {
		var err error
		destDir, err = ioutil.TempDir("", "ppa")
		if err != nil {
			return err
		}
	} else {
		err := os.MkdirAll(destDir, 0700)
		if err != nil {
			return err
		}
	}

	var wg sync.WaitGroup

	errC := make(chan error, len(fileURLs))
	wg.Add(len(fileURLs))
	for _, fileURL := range fileURLs {
		fileURL := fileURL

		go func() {
			defer wg.Done()

			client := http.Client{Timeout: timeout}
			base := path.Base(fileURL)
			fmt.Printf("downloading %s ...\n", base)
			destFile := filepath.Join(destDir, base)
			file, err := os.Create(destFile)
			if err != nil {
				errC <- fmt.Errorf("download error: file=%s, err=%s", base, err)
				return
			}
			defer file.Close()

			resp, err := client.Get(fileURL)
			if err != nil {
				errC <- fmt.Errorf("download error: file=%s, err=%s", base, err)
				return
			}
			defer resp.Body.Close()

			_, err = io.Copy(file, resp.Body)
			if err != nil {
				errC <- fmt.Errorf("download error: file=%s, err=%s", base, err)
				return
			}
			fmt.Printf("downloaded %s\n", base)
		}()
	}
	wg.Wait()

	close(errC)
	for err := range errC {
		fmt.Println(err)
	}

	log.Printf("downloaded files to %s", destDir)
	return nil
}
