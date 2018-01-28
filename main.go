package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/fsnotify/fsnotify"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
)

func init() {
	// Log as JSON instead of the default ASCII formatter.
	// log.SetFormatter(&log.JSONFormatter{})
	log.SetFormatter(&log.TextFormatter{FullTimestamp: true})

	// Output to stdout instead of the default stderr
	// Can be any io.Writer, see below for File example
	log.SetOutput(os.Stdout)

	// Only log the warning severity or above.
	log.SetLevel(log.InfoLevel)
}

type config struct {
	Port  string `mapstructure:"port"`
	User  string `mapstructure:"user"`
	Group string `mapstructure:"group"`
	Repos []repo
}

type repo struct {
	Url           string `mapstructure:"url"`
	Path          string `mapstructure:"path"`
	Directory     string `mapstructure:"directory"`
	Secret        string `mapstructure:"secret"`
	SshPrivKey    string `mapstructure:"sshPrivKey"`
	SshPassPhrase string `mapstructure:"sshPassPhrase"`
}

var C config

func (c *config) FindRepo(path string) (int, bool) {
	cleanPath := cleanURL(path)
	for k, repo := range c.Repos {
		if repo.Path == cleanPath {
			return k, true
		}
	}
	return 0, false
}

func cleanURL(url string) string {
	// strip trailing slash
	if url[len(url)-1] == '/' {
		return url[:len(url)-1]
	}
	return url
}

func (r *repo) Update() {
	fmt.Printf("r.Url = %+v\n", r.Url)
	fmt.Println(r.Name())
}

// short name for the logs
func (r *repo) Name() string {
	name := strings.TrimPrefix(r.Url, "git@github.com:")
	name = strings.TrimSuffix(name, ".git")
	return name
}

func handler(w http.ResponseWriter, r *http.Request) {

	if idx, ok := C.FindRepo(r.URL.Path); ok {
		C.Repos[idx].Update()
	}
}

// Pull changes from a remote repository
func main() {
	// setup config

	viper.SetConfigName("config")
	viper.AddConfigPath("/etc/gwg")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {
		log.Fatalf("Failed to read config file: %v\n", err)
	}

	if err := viper.Unmarshal(&C); err != nil {
		log.Panicf("Failed to setup configuration: %v\n", err)
	}

	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		fmt.Println("Config file changed:", e.Name)
		viper.Unmarshal(&C)
	})

	http.HandleFunc("/", handler)
	http.ListenAndServe(":8080", nil)

	// name := C.Name
	// fmt.Println("name: ", name)
	// repos := C.Repos
	// fmt.Printf("repo 1: %+v\n", repos)
	// urlpath := "/ghwh/diff/path"
	// idx, ok := findRepoIndex(urlpath)
	// if !ok {
	// 	log.Warnf("Could not find repo for path: %s", urlpath)
	// 	return
	// }
	// fmt.Printf("idx = %+v\n", idx)

	// os.Exit(0)

	// TODO
	// - break out into function
	// pass in path (repo dir), ssh priv key location
	// - create ssh auth connection
	repoLog := log.WithFields(log.Fields{
		"Repo": "mainRepo",
	})

	path := os.Args[1]
	_, err = os.Stat(path)
	if os.IsNotExist(err) {
		repoLog.Errorf("path %+v does not exist!\n", path)
		return
	}

	// We instance a new repository targeting the given path (the .git folder)
	r, err := git.PlainOpen(path)
	if err != nil {
		repoLog.Error("Failed to open local git repository")
		return
	}

	w, err := r.Worktree()
	if err != nil {
		repoLog.Errorf("Failed to open work tree for repository: %v\n", err)
		return
	}

	repoLog.Info("Fetching...")
	err = r.Fetch(&git.FetchOptions{RemoteName: "origin"})
	if err == git.NoErrAlreadyUpToDate {
		repoLog.Info("No new commits")
		return
	}
	if err != nil {
		repoLog.Errorf("Failed to fetch updates: %v\n", err)
		return
	}

	// Get local and remote refs to compare hashes before we proceed
	remoteRef, err := r.Reference(plumbing.ReferenceName("refs/remotes/origin/master"), true)
	if err != nil {
		repoLog.Errorf("Failed to get remote reference for remotes/origin/master: %v\n", err)
		return
	}
	localRef, err := r.Reference(plumbing.ReferenceName("HEAD"), true)
	if err != nil {
		repoLog.Errorf("Failed to get local reference for HEAD: %v\n", err)
		return
	}

	if remoteRef.Hash() == localRef.Hash() {
		repoLog.Warning("Already up to date")
		return
	}

	// git reset --hard [origin/master|hash]
	repoLog.Info("Resetting work tree....")
	err = w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteRef.Hash()})
	if err != nil {
		repoLog.Errorf("Failed to hard reset work tree: %v\n", err)
		return
	}

	// confirm changes
	repoLog.Info("Confirming changes....")
	headRef, err := r.Reference(plumbing.ReferenceName("HEAD"), true)
	if err != nil {
		repoLog.Errorf("Failed to get local HEAD reference: %v\n", err)
		return
	}

	if headRef.Hash() == remoteRef.Hash() {
		repoLog.Infof("Successfully updated local repository, latest hash: %v\n", headRef.Hash())
	} else {
		repoLog.Error("Something went wrong, hashes don't match!")
	}

	// print latest commit
	// commit, err := r.CommitObject(headRef.Hash())
	// if err != nil {
	// 	log.Warn("Failed to get local HEAD reference: %v\n", err)
	// 	return
	// }
	// fmt.Println(commit)

}
