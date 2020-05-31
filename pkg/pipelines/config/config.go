package config

import (
	"fmt"
	"path/filepath"
	"sort"
)

// PathForService gives a repo-rooted path within a repository.
func PathForService(env *Environment, serviceName string) string {
	return filepath.Join(PathForEnvironment(env), "services", serviceName)
}

// PathForApplication generates a repo-rooted path within a repository.
func PathForApplication(env *Environment, app *Application) string {
	return filepath.Join(PathForEnvironment(env), "apps", app.Name)
}

// PathForEnvironment gives a repo-rooted path within a repository.
func PathForEnvironment(env *Environment) string {
	return filepath.Join("environments", env.Name)
}

//PathForCICDEnvironment returns the path only for the CICD environment
func PathForCICDEnvironment(cicd *Cicd) string {
	return filepath.Join("config", cicd.Namespace)
}

func PathForArgoEnvironment() string {
	return filepath.Join("config", "argocd")
}

// Manifest describes a set of environments, apps and services for deployment.
type Manifest struct {
	GitOpsURL    string         `json:"gitops_url,omitempty"`
	Environments []*Environment `json:"environments,omitempty"`
	Config       *Special       `json:"config,omitempty"`
}

func (m *Manifest) GetEnvironment(n string) *Environment {
	for _, env := range m.Environments {
		if env.Name == n {
			return env
		}
	}
	return nil
}

func (m *Manifest) GetApplication(environment, application string) (*Application, error) {
	for _, env := range m.Environments {
		if env.Name == environment {
			for _, app := range env.Apps {
				if app.Name == application {
					return app, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("failed to find application: %s", application)
}

func (m *Manifest) AddService(envName, appName string, svc *Service) error {
	env := m.GetEnvironment(envName)
	if env == nil {
		return fmt.Errorf("environment %s does not exist", envName)
	}
	for _, service := range env.Services {
		if service.Name == svc.Name {
			return fmt.Errorf("service %s already exists at %s", svc.Name, env.Name)
		}
	}
	app, err := m.GetApplication(envName, appName)
	if app == nil && err != nil {
		app = &Application{Name: appName}
		env.Apps = append(env.Apps, app)
	}
	env.Services = append(env.Services, svc)
	app.ServiceRefs = append(app.ServiceRefs, svc.Name)
	return nil
}

// GetCICDEnvironment returns the CICD Environment if one exists.
func (m *Manifest) GetCICDEnvironment() (*Cicd, error) {
	if m.Config != nil {
		if m.Config.CICDEnv != nil {
			return m.Config.CICDEnv, nil
		}
	}
	return nil, nil
}

// GetArgoCDEnvironment returns the ArgoCD Environment if one exists.
func (m *Manifest) GetArgoCDEnvironment() (*Argo, error) {
	if m.Config != nil {
		if m.Config.ArgoCDEnv != nil {
			return m.Config.ArgoCDEnv, nil
		}
	}
	return nil, nil
}

// Environment is a slice of Apps, these are the named apps in the namespace.
//
// The CICD environment will be used to automatically generate CI/CD resources
// into.
// The CICD environment should not have any applications defined.
type Environment struct {
	Name      string         `json:"name,omitempty"`
	Pipelines *Pipelines     `json:"pipelines,omitempty"`
	Services  []*Service     `json:"services,omitempty"`
	Apps      []*Application `json:"apps,omitempty"`
	// TODO: this should check that there is 0 or 1 CICD environment in the
	// manfifest.
	IsCICD   bool `json:"cicd,omitempty"`
	IsArgoCD bool `json:"argo,omitempty"`
}

//Special are the special environments that constitute the argocd and cicd environment
type Special struct {
	CICDEnv   *Cicd `json:"cicdenv,omitempty"`
	ArgoCDEnv *Argo `json:"argocdenv,omitempty"`
}

//Cicd checks for the cicd environments
type Cicd struct {
	Namespace string `json:"namespace,omitempty"`
}

//Argo checks for the cicd environments
type Argo struct {
	Namespace string `json:"namespace,omitempty"`
}

// GoString return environment name
func (e Environment) GoString() string {
	return e.Name
}

// IsSpecial returns true if the environment is a special environment reserved
// for specific files.
// func (e Environment) IsSpecial() bool {
// 	return e.IsCICD || e.IsArgoCD
// }

// Application has many services.
//
// The ConfigRepo indicates that the configuration for this application lives in
// another repository.
// TODO: validate that an app with a ConfigRepo has no services.
type Application struct {
	Name        string      `json:"name,omitempty"`
	ServiceRefs []string    `json:"services,omitempty"`
	ConfigRepo  *Repository `json:"config_repo,omitempty"`
}

// Service has an upstream source.
type Service struct {
	Name      string     `json:"name,omitempty"`
	Webhook   *Webhook   `json:"webhook,omitempty"`
	SourceURL string     `json:"source_url,omitempty"`
	Pipelines *Pipelines `json:"pipelines,omitempty"`
}

// Webhook provides Github webhook secret for eventlisteners
type Webhook struct {
	Secret *Secret `json:"secret,omitempty"`
}

// Secret represents a K8s secret in a namespace
type Secret struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

// Repository refers to an upstream source for reading additional config from.
type Repository struct {
	URL string `json:"url,omitempty"`
	// TargetRevision defines the commit, tag, or branch in which to sync the application to.
	// If omitted, will sync to HEAD
	TargetRevision string `json:"target_revision,omitempty"`
	// Path is a directory path within the Git repository.
	Path string `json:"path,omitempty"`
}

// Pipelines describes the names for pipelines to be executed for CI and CD.
//
// These pipelines will be executed with a Git clone URL and commit SHA.
type Pipelines struct {
	Integration *TemplateBinding `json:"integration,omitempty"`
}

// TemplateBinding is a combination of the template and binding to be used for a
// pipeline execution.
type TemplateBinding struct {
	Template string   `json:"template,omitempty"`
	Bindings []string `json:"bindings,omitempty"`
}

// Walk implements post-node visiting of each element in the manifest.
//
// Every App, Service and Environment is called once, and any error from the
// handling function terminates the Walk.
//
// The environments are sorted using a custom sorting mechanism, that orders by
// name, but, moves CICD environments to the bottom of the list.
func (m Manifest) Walk(visitor interface{}) error {
	sort.Sort(ByName(m.Environments))
	for _, env := range m.Environments {
		for _, svc := range env.Services {
			if v, ok := visitor.(ServiceVisitor); ok {
				err := v.Service(env, svc)
				if err != nil {
					return err
				}
			}
		}

		for _, app := range env.Apps {
			if v, ok := visitor.(ApplicationVisitor); ok {
				err := v.Application(env, app)
				if err != nil {
					return err
				}
			}
		}
		if v, ok := visitor.(EnvironmentVisitor); ok {
			err := v.Environment(env)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type ByName []*Environment

func (a ByName) Len() int      { return len(a) }
func (a ByName) Swap(i, j int) { a[i], a[j] = a[j], a[i] }
func (a ByName) Less(i, j int) bool {
	// if a[i].IsSpecial() {
	// 	return false
	// }
	// if a[j].IsSpecial() {
	// 	return true
	// }
	return a[i].Name < a[j].Name
}
