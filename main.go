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
	Listen string `mapstructure:"listen"`
	Port   string `mapstructure:"port"`
	User   string `mapstructure:"user"`
	Group  string `mapstructure:"group"`
	Repos  []repo
}

type repo struct {
	URL           string `mapstructure:"url"`
	Path          string `mapstructure:"path"`
	Directory     string `mapstructure:"directory"`
	Secret        string `mapstructure:"secret"`
	SSHPrivKey    string `mapstructure:"sshPrivKey"`
	SSHPassPhrase string `mapstructure:"sshPassPhrase"`
	Trigger       string `mapstructure:"trigger"`
}

// C is global config
var C config

func (c *config) FindRepo(path string) (int, bool) {
	for k, repo := range c.Repos {
		if repo.Path == cleanURL(path) {
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
	repoLog := log.WithFields(log.Fields{
		"repo": r.Name(),
		"path": r.Path,
	})
	repoLog.Info("Starting update...")
	fmt.Printf("Url = %+v\nPath = %v\nDirectory = %v\nSecret = %v\nSSH Private Key = %v\n", r.URL, r.Path, r.Directory, r.Secret, r.SSHPrivKey)
	if r.HasSSHPassphrase() {
		fmt.Printf("SSHPassPhrase = %+v\n", r.SSHPassPhrase)
	}

	fmt.Printf("Trigger = %+v\n", r.Trigger)
	fmt.Println(r.Name())
	fmt.Println("=========================")
}

// short name for the logs
func (r *repo) Name() string {
	return strings.TrimSuffix((strings.TrimPrefix(r.URL, "git@github.com:")), ".git")
}

func isEmpty(field string) bool {
	if len(field) == 0 {
		return true
	}
	return false
}

func (r *repo) HasTrigger() bool {
	if isEmpty(r.Trigger) {
		return false
	}
	return true
}

func (r *repo) HasSecret() bool {
	if isEmpty(r.Secret) {
		return false
	}
	return true
}

func (r *repo) HasSSHPrivKey() bool {
	if isEmpty(r.SSHPrivKey) {
		return false
	}
	return true
}

func (r *repo) HasSSHPassphrase() bool {
	if isEmpty(r.SSHPassPhrase) {
		return false
	}
	return true
}

func handler(w http.ResponseWriter, r *http.Request) {
	if idx, ok := C.FindRepo(r.URL.Path); ok {
		// TODO: add semaphore and locks
		go C.Repos[idx].Update()
	} else {
		log.Warnf("Repository not found for path: %v\n", r.URL.Path)
	}
}

// Pull changes from a remote repository
func main() {
	// setup config
	viper.SetConfigName("config")
	viper.AddConfigPath("/etc/gwg")
	viper.AddConfigPath(".")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatalf("Failed to read config file: %v\n", err)
	}
	if err := viper.Unmarshal(&C); err != nil {
		log.Fatalf("Failed to setup configuration: %v\n", err)
	}

	// hot reloading can be improved, (adding mutexes might be overkill for now)
	viper.WatchConfig()
	viper.OnConfigChange(func(e fsnotify.Event) {
		log.Info("Config file changed:", e.Name)

		// update core config
		if viper.IsSet("user") {
			C.User = viper.GetString("user")
		} else {
			C.User = ""
		}
		if viper.IsSet("group") {
			C.Group = viper.GetString("group")
		} else {
			C.Group = ""
		}

		// update repo configs, we have to generate new repo configs incase old fields get removed or
		// commented out
		var newRepos []repo
		repos := viper.Get("repos")
		fmt.Printf("repo trigger (viper) = %+v\n", repos)
		for _, v := range repos.([]interface{}) {
			var newRepo repo
			for a, b := range v.(map[interface{}]interface{}) {
				// fmt.Printf("a = %+s, b= %+s\n", a, b)
				switch a {
				case "url":
					newRepo.URL = b.(string)
				case "path":
					newRepo.Path = b.(string)
				case "directory":
					newRepo.Directory = b.(string)
				case "trigger":
					newRepo.Trigger = b.(string)
				case "secret":
					newRepo.Secret = b.(string)
				case "sshPrivKey":
					newRepo.SSHPrivKey = b.(string)
				case "sshPassPhrase":
					newRepo.SSHPassPhrase = b.(string)
				}
			}
			newRepos = append(newRepos, newRepo)
		}
		C.Repos = newRepos
		// viper.Unmarshal(&C)
		// old fields remain if commented out!
		// have to rebuild or blank out existing values
	})

	// listen and port changes require a restart
	http.HandleFunc("/", handler)
	http.ListenAndServe(C.Listen+":"+C.Port, nil)

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
	_, err := os.Stat(path)
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
