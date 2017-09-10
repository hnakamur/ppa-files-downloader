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
	c := flag.Int("c", 6, "download concurrency")
	timeout := flag.Duration("timeout", time.Minute, "http client timeout")
	flag.Parse()

	buildURL, err := getBuildURL(*user, *repo, *pkg)
	if err != nil {
		log.Fatal(err)
	}
	fileURLs, err := getFileURLs(buildURL)
	if err != nil {
		log.Fatal(err)
	}
	err = downloadFiles(*c, fileURLs, *timeout, *destDir)
	if err != nil {
		log.Fatal(err)
	}
}

func getBuildURL(user, repo, pkg string) (*url.URL, error) {
	packagesURL, err := url.Parse(fmt.Sprintf("https://launchpad.net/~%s/+archive/ubuntu/%s/+packages?nocache_dummy=%d", user, repo, time.Now().Unix()))
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocument(packagesURL.String())
	if err != nil {
		return nil, err
	}
	var buildURL *url.URL
	doc.Find("a.expander").Each(func(i int, s *goquery.Selection) {
		words := strings.Split(strings.TrimSpace(s.Text()), " - ")
		if len(words) != 2 || words[0] != pkg {
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

func downloadFiles(concurrency int, fileURLs []string, timeout time.Duration, destDir string) error {
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
	jobs := make(chan string)
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			client := http.Client{Timeout: timeout}
			for job := range jobs {
				base := path.Base(job)
				destFile := filepath.Join(destDir, base)
				file, err := os.Create(destFile)
				if err != nil {
					log.Println(err)
				}
				defer file.Close()

				resp, err := client.Get(job)
				if err != nil {
					log.Println(err)
				}
				defer resp.Body.Close()
				_, err = io.Copy(file, resp.Body)
				if err != nil {
					log.Println(err)
				}
				log.Printf("downloaded %s", job)
			}
		}()
	}

	for _, fileURL := range fileURLs {
		jobs <- fileURL
	}
	close(jobs)
	wg.Wait()
	log.Printf("downloaded files to %s", destDir)
	return nil
}
