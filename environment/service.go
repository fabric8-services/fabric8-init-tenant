package environment

import (
	"context"
	"errors"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	"github.com/fabric8-services/fabric8-common/log"
	"github.com/fabric8-services/fabric8-tenant/environment/generated"
	"github.com/fabric8-services/fabric8-tenant/toggles"
	"github.com/fabric8-services/fabric8-tenant/utils"
	goajwt "github.com/goadesign/goa/middleware/security/jwt"
	"path"
	"strconv"
	"strings"
	"time"
)

//go:generate go-bindata -prefix "./templates/" -pkg templates -o ./generated/templates.go ./templates/...

const (
	f8TenantServiceRepoUrl = "https://raw.githubusercontent.com/fabric8-services/fabric8-tenant"
	rawFileURLTemplate     = "%s/%s/%s"
	templatesDirectory     = "environment/templates/"
)

var (
	VersionFabric8TenantUserFile    string
	VersionFabric8TenantCheMtFile   string
	VersionFabric8TenantJenkinsFile string
	VersionFabric8TenantCheFile     string
	VersionFabric8TenantDeployFile  string
	DefaultEnvTypes                 = []string{"che", "jenkins", "user", "run", "stage"}
)

func retrieveMappedTemplates() map[string][]*Template {
	return map[string][]*Template{
		"run":     tmpls(deploy("run"), "fabric8-tenant-deploy.yml"),
		"stage":   tmpls(deploy("stage"), "fabric8-tenant-deploy.yml"),
		"che-mt":  tmpls(version(VersionFabric8TenantCheMtFile), "fabric8-tenant-che-mt.yml", "fabric8-tenant-che-quotas.yml"),
		"che":     tmpls(version(VersionFabric8TenantCheFile), "fabric8-tenant-che.yml", "fabric8-tenant-che-quotas.yml"),
		"jenkins": tmpls(version(VersionFabric8TenantJenkinsFile), "fabric8-tenant-jenkins.yml", "fabric8-tenant-jenkins-quotas.yml"),
		"user":    tmpls(version(VersionFabric8TenantUserFile), "fabric8-tenant-user.yml"),
	}
}

func version(version string) map[string]string {
	return map[string]string{varCommit: version}
}

func deploy(stage string) map[string]string {
	return map[string]string{
		varCommit:     VersionFabric8TenantDeployFile,
		varDeployType: stage,
	}
}

func tmpls(defaultParams map[string]string, filenames ...string) []*Template {
	tmpls := make([]*Template, 0, len(filenames))
	for _, fileName := range filenames {
		tmpls = append(tmpls, newTemplate(fileName, defaultParams))
	}
	return tmpls
}

type Service struct {
	templatesRepo     string
	templatesRepoBlob string
	templatesRepoDir  string
}

func NewService(templatesRepo, templatesRepoBlob, templatesRepoDir string) *Service {
	return &Service{
		templatesRepo:     templatesRepo,
		templatesRepoBlob: templatesRepoBlob,
		templatesRepoDir:  templatesRepoDir,
	}
}

type EnvData struct {
	NsType    string
	Name      string
	Templates []*Template
}

func (s *Service) GetEnvData(ctx context.Context, envType string) (*EnvData, error) {
	var templates []*Template
	var mappedTemplates = retrieveMappedTemplates()
	if envType == "che" {
		if toggles.IsEnabled(ctx, "deploy.che-multi-tenant", false) {
			cheMtParams, err := getCheMtParams(ctx)
			if err != nil {
				return nil, err
			}
			templates = mappedTemplates["che-mt"]
			templates[0].DefaultParams = cheMtParams
		} else {
			templates = mappedTemplates[envType]
		}
	} else {
		templates = mappedTemplates[envType]
	}

	err := s.retrieveTemplates(templates)
	if err != nil {
		return nil, err
	}

	return &EnvData{
		Templates: templates,
		Name:      envType,
		NsType:    envType,
	}, nil
}

func getCheMtParams(ctx context.Context) (map[string]string, error) {
	token := goajwt.ContextJWT(ctx)
	cheMtParams := map[string]string{}
	if token != nil {
		cheMtParams["OSIO_TOKEN"] = token.Raw
		id := token.Claims.(jwt.MapClaims)["sub"]
		if id == nil {
			return nil, errors.New("missing sub in JWT token")
		}
		cheMtParams["IDENTITY_ID"] = id.(string)
	}
	cheMtParams["REQUEST_ID"] = log.ExtractRequestID(ctx)
	unixNano := time.Now().UnixNano()
	cheMtParams["JOB_ID"] = strconv.FormatInt(unixNano/1000000, 10)

	return cheMtParams, nil
}

func (s *Service) retrieveTemplates(tmpls []*Template) error {
	var (
		content []byte
		err     error
	)
	for _, template := range tmpls {
		if s.templatesRepoBlob != "" {
			fileURL := fmt.Sprintf(rawFileURLTemplate, s.getRepo(), s.templatesRepoBlob, s.getPath(template))
			content, err = utils.DownloadFile(fileURL)
			template.DefaultParams[varCommit] = s.templatesRepoBlob
		} else {
			content, err = templates.Asset(template.Filename)
		}
		if err != nil {
			return err
		}
		template.Content = string(content)
	}
	return nil
}

func (s *Service) getRepo() string {
	repo := strings.TrimSpace(s.templatesRepo)
	if repo == "" {
		return f8TenantServiceRepoUrl
	}
	if strings.Contains(repo, "github.com") {
		return strings.Replace(repo, "github.com", "raw.githubusercontent.com", 1)
	}
	return repo
}

func (s *Service) getPath(template *Template) string {
	directory := strings.TrimSpace(s.templatesRepoDir)
	if directory == "" {
		directory = templatesDirectory
	}
	return path.Clean(directory + "/" + template.Filename)
}
