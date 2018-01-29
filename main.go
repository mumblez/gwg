package main

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/google/go-github/github"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing"
	"gopkg.in/src-d/go-git.v4/plumbing/transport/ssh"
)

func init() {
	// Log as JSON instead of the default ASCII formatter..
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
	Branch        string `mapstructure:"branch"`
	Secret        string `mapstructure:"secret"`
	SSHPrivKey    string `mapstructure:"sshPrivKey"`
	SSHPassPhrase string `mapstructure:"sshPassPhrase"`
	Trigger       string `mapstructure:"trigger"`
}

// C is global config
var C config

func (c *config) FindRepo(path string) (int, bool) {
	for r, repo := range c.Repos {
		if repo.Path == cleanURL(path) {
			return r, true
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

func (r *repo) clone() {
	rlog := log.WithFields(log.Fields{
		"repo": r.Name(),
		"path": r.Path,
	})
	sshAuth, err := ssh.NewPublicKeysFromFile("git", r.SSHPrivKey, r.SSHPassPhrase)
	if err != nil {
		rlog.Errorf("Failed to setup ssh auth: %v\n", err)
		return
	}

	_, err = git.PlainClone(r.Directory, false, &git.CloneOptions{
		URL:           r.URL,
		ReferenceName: plumbing.ReferenceName("refs/heads/" + r.Branch),
		SingleBranch:  true,
		Auth:          sshAuth,
	})
	if err != nil {
		rlog.Errorf("Failed to clone repository: %v\n", err)
		return
	}
	rlog.Info("Cloned repository")

	if r.Trigger == "" {
		return
	}
	if err := r.touchTrigger(); err != nil {
		log.Errorf("Failed to update trigger file: %v\n")
		return
	}
	log.Info("Successfully updated trigger file")
}

// essentially git fetch and git reset --hard origin/master | latest remote commit
func (r *repo) update() {
	rlog := log.WithFields(log.Fields{
		"repo": r.Name(),
		"path": r.Path,
	})
	sshAuth, err := ssh.NewPublicKeysFromFile("git", r.SSHPrivKey, r.SSHPassPhrase)
	if err != nil {
		rlog.Errorf("Failed to setup ssh auth: %v\n", err)
		return
	}

	repo, err := git.PlainOpen(r.Directory)
	if err != nil {
		rlog.Errorf("Failed to open local git repository: %v\n", err)
		return
	}

	w, err := repo.Worktree()
	if err != nil {
		rlog.Errorf("Failed to open work tree for repository: %v\n", err)
		return
	}

	// TODO: assume origin?
	err = repo.Fetch(&git.FetchOptions{
		RemoteName: "origin",
		Auth:       sshAuth,
	})
	if err == git.NoErrAlreadyUpToDate {
		rlog.Info("No new commits")
		return
	}
	if err != nil {
		rlog.Errorf("Failed to fetch updates: %v\n", err)
		return
	}
	rlog.Info("Fetched new updates")

	// Get local and remote refs to compare hashes before we proceed
	remoteRef, err := repo.Reference(plumbing.ReferenceName("refs/remotes/origin/"+r.Branch), true)
	if err != nil {
		rlog.Errorf("Failed to get remote reference for remotes/origin/%s: %v\n", r.Branch, err)
		return
	}
	localRef, err := repo.Reference(plumbing.ReferenceName("HEAD"), true)
	if err != nil {
		rlog.Errorf("Failed to get local reference for HEAD: %v\n", err)
		return
	}

	if remoteRef.Hash() == localRef.Hash() {
		rlog.Warning("Already up to date")
		return
	}

	// git reset --hard [origin/master|hash]
	err = w.Reset(&git.ResetOptions{Mode: git.HardReset, Commit: remoteRef.Hash()})
	if err != nil {
		rlog.Errorf("Failed to hard reset work tree: %v\n", err)
		return
	}
	rlog.Info("Work tree up to date")

	rlog.Info("Confirming changes....")
	headRef, err := repo.Reference(plumbing.ReferenceName("HEAD"), true)
	if err != nil {
		rlog.Errorf("Failed to get local HEAD reference: %v\n", err)
		return
	}

	if headRef.Hash() == remoteRef.Hash() {
		rlog.Infof("Successfully updated local repository, latest hash: %v\n", headRef.Hash())
	} else {
		rlog.Error("Something went wrong, hashes don't match!")
		return
	}

	if r.Trigger == "" {
		return
	}
	if err := r.touchTrigger(); err != nil {
		log.Errorf("Failed to update trigger file: %v\n")
		return
	}
	log.Info("Successfully updated trigger file")
}

func (r *repo) touchTrigger() error {
	return os.Chtimes(r.Trigger, time.Now(), time.Now())
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
	idx, ok := C.FindRepo(r.URL.Path)
	if !ok {
		log.Warnf("Repository not found for path: %v\n", r.URL.Path)
		return
	}

	payload, err := github.ValidatePayload(r, []byte(C.Repos[idx].Secret))
	defer r.Body.Close()
	if err != nil {
		log.Errorf("Error validating request body: %v\n", err)
		return
	}

	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		log.Errorf("Could not parse webhook: %v\n", err)
		return
	}

	switch e := event.(type) {
	case *github.PushEvent:
		if C.Repos[idx].URL == *e.Repo.SSHURL && C.Repos[idx].Branch == strings.TrimPrefix(*e.Ref, "refs/heads/") {
			if _, err := os.Stat(C.Repos[idx].Directory); err != nil {
				go C.Repos[idx].clone()
			} else {
				go C.Repos[idx].update()
			}
		} else {
			log.WithFields(log.Fields{
				"URL": *e.Repo.SSHURL,
				"Ref": *e.Ref,
			}).Warn("Push event did not match our configuration")
		}
		return
	default:
		log.Warnf("Unknown event type %v\n", github.WebHookType(r))
		return
	}
}

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
		log.Warn("Config file changed: ", e.Name)

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
		for _, v := range repos.([]interface{}) {
			var newRepo repo
			for a, b := range v.(map[interface{}]interface{}) {
				switch a {
				case "url":
					newRepo.URL = b.(string)
				case "path":
					newRepo.Path = b.(string)
				case "directory":
					newRepo.Directory = b.(string)
				case "branch":
					newRepo.Branch = b.(string)
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
			// defaults
			if newRepo.Branch == "" {
				newRepo.Branch = "master"
			}
			newRepos = append(newRepos, newRepo)
		}
		C.Repos = newRepos
		// viper.Unmarshal(&C)
		// old fields remain if commented out!
		// have to rebuild or blank out existing values
		log.Warn("Configuration updated")
	})

	// listen and port changes require a restart
	http.HandleFunc("/", handler)
	http.ListenAndServe(C.Listen+":"+C.Port, nil)

}
