package main

import (
	"context"
	"net/http"
	"strings"
	"os"
	"io"
	"errors"
	"github.com/google/go-github/v34/github"
	"golang.org/x/oauth2"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"fmt"
	"io/ioutil"
	"regexp"
	"flag"
	"github.com/bradleyfalzon/ghinstallation"
)

type webhookHandler struct {
	Config *Config
}
type Command struct {
	Name string `yaml:"name"`
	Desc string `yaml:"description"`
}
type Commands struct {
	List []Command `yaml:"command"`
	Prefix string `yaml:"prefix"`
}
type Github struct {
	Token string `yaml:"token"`
	AppId int64 `yaml:"appId"`
	AppInstallation int64 `yaml:"appInstallation"`
	AppPrivateKey string `yaml:"appPrivateKey"`
	PullRequests *PullRequests `yaml:"pullRequests"`
}
type PullRequests struct {
	CheckTitle bool `yaml:"checkTitle"`
	CheckBody bool `yaml:"checkBody"`
	Commands *Commands `yaml:"commands"`
}
type Server struct {
	Port string `yaml:"port"`
	Address string `yaml:"address"`
}
type Logging struct {
	Level string `yaml:"level"`
}
type Config struct {
	Github *Github `yaml:"github"`
	Server *Server `yaml:"server"`
	Logging *Logging `yaml:"logging"`
}

func (c *Commands) getCommand (name string) (*Command, error) {
	for _, command := range c.List {
		if(command.Name == name) {
			return &command, nil
		}
	}
	return nil, errors.New("command not found")
}

func (c *Command) execute(command []string, event *github.IssueCommentEvent, config *Config, client *github.Client) (error) {
	log.WithFields(log.Fields{
		"PR" : event.GetIssue().GetNumber(),
		"repo" : event.GetRepo().GetName(),
		"command" : c.Name,
	}).Debug("received set command event")
	switch (c.Name) {
		case "lgtm":
			log.Info("LGTM Command")
		default:
			return nil
	}
	return nil
}

func getEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultVal
}

func Find(slice []string, val string) (int, bool, error) {
    for i, item := range slice {
        if item == val {
            return i, true, nil
        }
    }
    return -1, false, errors.New("element not found in array")
}

func parseConfig(filename string) (*Config, error) {
	file, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	config := &Config{}
	err = yaml.Unmarshal(file, config)
        if err != nil {
		 return nil, fmt.Errorf("in file %q: %v", file, err)
        }
	return config, nil
}

func handleComments(event *github.IssueCommentEvent, client *github.Client, c *Config) (error) {
	commandStr := event.GetComment().GetBody()
	if event.GetIssue().IsPullRequest() {
		if strings.HasPrefix(commandStr, c.Github.PullRequests.Commands.Prefix) {
			commandStr = strings.TrimPrefix(commandStr, c.Github.PullRequests.Commands.Prefix)
			commandArray := strings.Split(commandStr, " ")
			command, err := c.Github.PullRequests.Commands.getCommand(commandArray[0])
			if err != nil {
				return errors.New("invalid command, please view the command list")
			}

			err = command.execute(commandArray, event, c, client)
			if err != nil {
				log.Error(err)
				return errors.New("error executing a command")
			}
		}
	}
	return nil
}

func validateRegex(text string, regex string)(bool){
	var valid = regexp.MustCompile(regex)
	return valid.MatchString(text)
}

func validateBody(text string, regex string)(bool){
	var valid = regexp.MustCompile(regex)
	substring := valid.FindStringSubmatch(text)
	if len(substring) > 0 {
		description := strings.TrimSpace(valid.FindStringSubmatch(text)[1])
		if len(description) > 0 && description != "Please describe your pull request." {
			return true
		}
	}
	return false
}

func createCheck(client *github.Client, owner string, repo string, sha, name string, valid bool){
	var conclusion = "skipped"
	if valid == true {
		conclusion = "success"
	} else {
		conclusion = "failure"
	}

	opts := github.CreateCheckRunOptions{
		Name:       name,
		HeadSHA:    sha,
		Status:     github.String("completed"),
		Conclusion: github.String(conclusion),
	 }

	 _, _, err := client.Checks.CreateCheckRun(context.Background(), owner, repo, opts)
	 if err != nil {
		log.Error(err)
	}
}

func handlePullRequest(event *github.PullRequestEvent, client *github.Client, c *Config) (error) {
	var (
		title = event.GetPullRequest().GetTitle()
		body = event.GetPullRequest().GetBody()
		owner = event.GetRepo().GetOwner().GetLogin()
		repo = event.GetRepo().GetName()
		sha = event.GetPullRequest().Head.GetSHA()
		regexTitle = `^(build|chore|ci|docs|feat|fix|perf|refactor|revert|style|test)(\([a-z ]+\))?(!)?: [\w ]+$`
		regexBody = `(?s)## Description(.*)## Other information`
	)
	if c.Github.PullRequests.CheckTitle {
		isValidTitle := validateRegex(title, regexTitle)
		createCheck(client, owner, repo, sha, "Pull Request Title", isValidTitle)
	}
	if c.Github.PullRequests.CheckBody {
		isValidBody := validateBody(body,regexBody)
		createCheck(client, owner, repo, sha, "Pull Request Description", isValidBody)
	}
	return nil
}

func newClient(githubToken string) (*github.Client){
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: githubToken},
	)
	tc := oauth2.NewClient(context.Background(), ts)
	return github.NewClient(tc)
}

func newAppClient(githubConfig *Github)(*github.Client){
	tr := http.DefaultTransport
	key := []byte(githubConfig.AppPrivateKey)

	itr, err := ghinstallation.New(tr, githubConfig.AppId, githubConfig.AppInstallation, key)
	if err != nil {
		log.Fatal(err)
	}
	return github.NewClient(&http.Client{Transport: itr})
}


func (wh *webhookHandler) handler(w http.ResponseWriter, r *http.Request) {
	githubAppClient := newAppClient(wh.Config.Github)
	payload, err := github.ValidatePayload(r, []byte(""))
	if err != nil {
		log.Error("error validating request body: err=%s\n", err)
		return
	}
	defer r.Body.Close()
	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		log.Error("could not parse webhook: err=%s\n", err)
		return
	}
	switch e := event.(type) {
		case *github.IssueCommentEvent:
			err := handleComments(e, githubAppClient, wh.Config)
			if err != nil {
				log.Error(err)
				return
			}
        case *github.PullRequestEvent:
            err := handlePullRequest(e, githubAppClient, wh.Config)
            if err != nil {
                log.Error(err)
                return
            }
		default:
			return
	}
}

func main() {
	var configPath string
    flag.StringVar(&configPath, "config", "config.yaml", "path of configuration file")
	flag.Parse()
	log.Info("configuration file: ",configPath)
	config, err := parseConfig(configPath)
    if err != nil {
		log.Fatal(err)
    }
    lvl := config.Logging.Level
    ll, err := log.ParseLevel(lvl)
    if err != nil {
        ll = log.DebugLevel
    }
    log.SetLevel(ll)
	log.Info("server listening on " + config.Server.Address + ":" + config.Server.Port)
	webhookHandlerMain := &webhookHandler{Config: config}
	http.HandleFunc("/webhook", webhookHandlerMain.handler)
	http.HandleFunc("/healthz",func(rw http.ResponseWriter, r *http.Request) { io.WriteString(rw, "Ok") })
	log.Fatal(http.ListenAndServe(":" + config.Server.Port, nil))
}
