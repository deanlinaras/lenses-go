package lenses

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	currentContextKeyJSON = "currentContext"
	currentContextKeyYAML = "CurrentContext"

	contextsKeyJSON = "contexts"
	contextsKeyYAML = "Contexts"

	basicAuthenticationKeyJSON = "basic_authentication"
	basicAuthenticationKeyYAML = "BasicAuthentication"

	kerberosAuthenticationKeyJSON = "kerberos_authentication"
	kerberosAuthenticationKeyYAML = "KerberosAuthentication"
)

type (
	// Configuration contains the necessary information
	// that `OpenConnection` needs to create a new client which connects and talks to the lenses backend box.
	//
	// Optionally, the `Contexts` map of string and client configuration values can be filled to map different environments.
	// Use of `WithContext` `ConnectionOption` to select a specific `ClientConfiguration`, otherwise the first one is selected,
	// this will also amend the `CurrentContext` via the top-level `OpenConnection` function.
	//
	// Configuration can be loaded via JSON or YAML.
	Configuration struct {
		CurrentContext string
		Contexts       map[string]*ClientConfiguration
	}

	// ClientConfiguration contains the necessary information to a client to connect to the lenses backend box.
	ClientConfiguration struct {
		// Host is the network shema  address and port that your lenses backend box is listening on.
		Host string `json:"host" yaml:"Host" survey:"host"`

		// Authentication, in order to gain access using different kind of options.
		//
		// See `BasicAuthentication` and `KerberosAuthentication` or the example for more.
		Authentication Authentication `json:"-" yaml:"-" survey:"-"`

		// Token is the "X-Kafka-Lenses-Token" request header's value.
		// If not empty, overrides any `Authentication` settings.
		//
		// If `Token` is expired then all the calls will result on 403 forbidden error HTTP code
		// and a manual renewal will be demanded.
		//
		// For general-purpose usecase the recommendation is to let this field empty and
		// fill the `Authentication` field instead.
		Token string `json:"token,omitempty" yaml:"Token" survey:"-"`

		// Timeout specifies the timeout for connection establishment.
		//
		// Empty timeout value means no timeout.
		//
		// Such as "300ms", "-1.5h" or "2h45m".
		// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
		// Example: "5s" for 5 seconds, "5m" for 5 minutes and so on.
		Timeout string `json:"timeout,omitempty" yaml:"Timeout" survey:"timeout"`
		// Debug activates the debug mode, it logs every request, the configuration (except the `Password`)
		// and its raw response before decoded but after gzip reading.
		//
		// If this is enabled then printer's contents are not predicted to the end-user.
		// The output source is always the `os.Stdout` which 99.9% of the times means the terminal,
		// so use it only for debugging.
		//
		//
		// Defaults to false.
		Debug bool `json:"debug,omitempty" yaml:"Debug" survey:"debug"`
	}
)

// IsValid returns the result of the contexts' ClientConfiguration#IsValid.
func (c *Configuration) IsValid() bool {
	// for a whole configuration to be valid we need to check each contexts' configs as well.
	if len(c.Contexts) == 0 {
		return false
	}

	for _, cfg := range c.Contexts {
		if !cfg.IsValid() {
			return false
		}
	}

	return len(c.Contexts) > 0
}

// IsValid returns true if the configuration contains the necessary fields, otherwise false.
func (c *ClientConfiguration) IsValid() bool {
	if len(c.Host) == 0 {
		return false
	}

	c.FormatHost()

	return c.Host != "" && (c.Token != "" || c.Authentication != nil)
}

// DefaultContextKey is used to set an empty client configuration when no custom context available.
var DefaultContextKey = "master"

// GetCurrent returns the specific current client configuration based on the `CurrentContext`.
func (c *Configuration) GetCurrent() *ClientConfiguration {
	if c.Contexts == nil {
		c.Contexts = make(map[string]*ClientConfiguration)
	}

	if cfg, has := c.Contexts[c.CurrentContext]; has {
		// c.FormatHost()
		return cfg
	}

	cfg := new(ClientConfiguration)
	if c.CurrentContext == "" {
		c.CurrentContext = DefaultContextKey // the default one if missing.
	}

	c.Contexts[c.CurrentContext] = cfg
	return cfg
}

// RemoveTokens removes the `Token` from all client configurations.
func (c *Configuration) RemoveTokens() {
	for _, v := range c.Contexts {
		v.Token = ""
	}
}

// SetCurrent overrides the `CurrentContext`, just this.
func (c *Configuration) SetCurrent(currentContextName string) {
	c.CurrentContext = currentContextName
}

// CurrentContextExists just checks if the `CurrentContext` exists in the `Contexts` map.
func (c *Configuration) CurrentContextExists() bool {
	_, exists := c.Contexts[c.CurrentContext]
	return exists
}

// RemoveContext deletes a context based on its name/key.
// It will change if there is an available context to set as current, if can't find then the operation stops.
// Returns true if found and removed and can change to something valid, otherwise false.
func (c *Configuration) RemoveContext(contextName string) bool {
	if _, ok := c.Contexts[contextName]; ok {

		canBeRemoved := false
		// we are going to remove the current context, let's check if we can change the current context to a valid one first.
		if c.CurrentContext == contextName {
			for name, cfg := range c.Contexts {
				if name == contextName {
					continue // skip the context we want to delete of course.
				}
				if cfg.IsValid() { // set the current to the first valid one.
					canBeRemoved = true
					c.SetCurrent(name)
					break
				}
			}
		} else {
			canBeRemoved = true
		}

		if canBeRemoved {
			delete(c.Contexts, contextName)
		}

		return canBeRemoved
	}

	return false
}

// Clone will returns a deep clone of the this `Configuration`.
func (c *Configuration) Clone() Configuration {
	clone := Configuration{CurrentContext: c.CurrentContext}
	clone.Contexts = make(map[string]*ClientConfiguration, len(c.Contexts))
	for k, v := range c.Contexts {
		vCopy := *v
		clone.Contexts[k] = &vCopy
	}

	return clone
}

// FillCurrent fills the specific client configuration based on the `CurrentContext` if it's valid.
func (c *Configuration) FillCurrent(cfg ClientConfiguration) {
	context := c.CurrentContext

	if _, ok := c.Contexts[context]; !ok {
		if cfg.IsValid() {
			c.Contexts[context] = &cfg
		}
	} else {
		c.Contexts[context].Fill(cfg)
	}
}

// Fill iterates over the "other" ClientConfiguration's fields
// it checks if a field is not empty,
// if it's then it sets the value to the "c" ClientConfiguration's particular field.
//
// It returns true if the final configuration is valid by calling the `IsValid`.
func (c *ClientConfiguration) Fill(other ClientConfiguration) bool {
	if v := other.Host; v != "" && v != c.Host {
		c.Host = v
	}

	if other.Authentication != nil {
		c.Authentication = other.Authentication
	}

	if v := other.Token; v != "" && v != c.Token {
		c.Token = v
	}

	if v := other.Timeout; v != "" && v != c.Timeout {
		c.Timeout = v
	}

	if c.Debug != other.Debug {
		c.Debug = other.Debug
	}

	return c.IsValid()
}

// FormatHost will try to make sure that the schema:host:port pattern is followed on the `Host` field.
func (c *ClientConfiguration) FormatHost() {
	if len(c.Host) == 0 {
		return
	}

	// remove last slash, so the API can append the path with ease.
	if c.Host[len(c.Host)-1] == '/' {
		c.Host = c.Host[0 : len(c.Host)-1]
	}

	portIdx := strings.LastIndexByte(c.Host, ':')

	schemaIdx := strings.Index(c.Host, "://")
	hasSchema := schemaIdx >= 0
	hasPort := portIdx > schemaIdx+1

	var port = "80"
	if hasPort {
		port = c.Host[portIdx+1:]
	}

	// find the schema based on the port.
	if !hasSchema {
		if port == "443" {
			c.Host = "https://" + c.Host
		} else {
			c.Host = "http://" + c.Host
		}
	} else if !hasPort {
		// has schema but not port.
		if strings.HasPrefix(c.Host, "https://") {
			port = "443"
		}
	}

	// finally, append the port part if it wasn't there.
	if !hasPort {
		c.Host += ":" + port
	}
}

// IsBasicAuth reports whether the authentication is basic.
func (c *ClientConfiguration) IsBasicAuth() (BasicAuthentication, bool) {
	auth, isBasicAuth := c.Authentication.(BasicAuthentication)
	return auth, isBasicAuth
}

// IsKerberosAuth reports whether the authentication is kerberos-based.
func (c *ClientConfiguration) IsKerberosAuth() (KerberosAuthentication, bool) {
	auth, isKerberosAuth := c.Authentication.(KerberosAuthentication)
	return auth, isKerberosAuth
}

// UnmarshalFunc is the most standard way to declare a Decoder/Unmarshaler to read the configurations and more.
// See `ReadConfiguration` and `ReadConfigurationFromFile` for more.
type UnmarshalFunc func(in []byte, outPtr *Configuration) error

// ReadConfiguration reads and decodes Configuration from an io.Reader based on a custom unmarshaler.
// This can be useful to read configuration via network or files (see `ReadConfigurationFromFile`).
// Sets the `outPtr`. Retruns a non-nil error on any unmarshaler's errors.
func ReadConfiguration(r io.Reader, unmarshaler UnmarshalFunc, outPtr *Configuration) error {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}

	return unmarshaler(data, outPtr)
}

// ReadConfigurationFromFile reads and decodes Configuration from a file based on a custom unmarshaler,
// `ReadConfigurationFromJSON` and `ReadConfigurationFromYAML` are the internal users,
// but the end-developer can use any custom type of decoder to read a configuration file with ease using this function,
// but keep note that the default behavior of the fields depend on the existing unmarshalers, use these tag names to map
// your decoder's properties.
//
// Accepts the absolute or the relative path of the configuration file.
// Sets the `outPtr`. Retruns a non-nil error if parsing or decoding the file failed or file doesn't exist.
func ReadConfigurationFromFile(filename string, unmarshaler UnmarshalFunc, outPtr *Configuration) error {
	// get the abs
	// which will try to find the 'filename' from current working dir as well.
	absPath, err := filepath.Abs(filename)
	if err != nil {
		return err
	}

	f, err := os.Open(absPath)
	if err != nil {
		return err
	}

	err = ReadConfiguration(f, unmarshaler, outPtr)
	f.Close()
	return err
}

// TryReadConfigurationFromFile will try to read a specific file and unmarshal to `Configuration`.
// It will try to read it with one of these built'n lexers/formats:
// 1. JSON
// 2. YAML
func TryReadConfigurationFromFile(filename string, outPtr *Configuration) (err error) {
	tries := []UnmarshalFunc{
		ConfigurationUnmarshalJSON,
		ConfigurationUnmarshalYAML,
	}

	for _, unmarshaler := range tries {
		err = ReadConfigurationFromFile(filename, unmarshaler, outPtr)
		if err == nil { // if decoded without any issues, then return that as soon as possible.
			return
		}
	}

	return fmt.Errorf("configuration file '%s' does not exist or it is not formatted to a compatible document: JSON, YAML", filename)
}

var configurationPossibleFilenames = []string{
	"lenses.yml", "lenses.yaml", "lenses.json",
	".lenses.yml", ".lenses.yaml", ".lenses.json",
	// client and cli can share the exactly configuration if caller loads from home dir.
	"lenses-cli.yml", "lenses-cli.yaml", "lenses-cli.json",
	".lenses-cli.yml", ".lenses-cli.yaml", ".lenses-cli.json",
} // no patterns in order to be easier to remove or modify these.

func lookupConfiguration(dir string, outPtr *Configuration) bool {
	for _, filename := range configurationPossibleFilenames {
		fullpath := filepath.Join(dir, filename)
		err := TryReadConfigurationFromFile(fullpath, outPtr)
		if err == nil {
			return true
		}
	}

	return false
}

// HomeDir returns the home directory for the current user on this specific host machine.
func HomeDir() (homeDir string) {
	u, err := user.Current() // ignore error handler.

	if u != nil && err == nil {
		homeDir = u.HomeDir
	}

	if homeDir == "" {
		homeDir = os.Getenv("HOME")
	}

	if homeDir == "" {
		if runtime.GOOS == "plan9" {
			homeDir = os.Getenv("home")
		} else if runtime.GOOS == "windows" {
			homeDir = os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
			if homeDir == "" {
				homeDir = os.Getenv("USERPROFILE")
			}
		}
	}

	return
}

// DefaultConfigurationHomeDir is the default configuration system directory,
// by default it's the $HOME/.lenses directory.
var DefaultConfigurationHomeDir = filepath.Join(HomeDir(), ".lenses")

// TryReadConfigurationFromHome will try to read the `Configuration`
// from the current user's home directory/.lenses, the lookup is based on
// the common configuration filename pattern:
// lenses-cli.json, lenses-cli.yml, lenses-cli.yml or lenses.json, lenses.yml and lenses.tml.
func TryReadConfigurationFromHome(outPtr *Configuration) bool {
	return lookupConfiguration(DefaultConfigurationHomeDir, outPtr)
}

// TryReadConfigurationFromExecutable will try to read the `Configuration`
// from the (client's caller's) executable path that started the current process.
// The lookup is based on the common configuration filename pattern:
// lenses-cli.json, lenses-cli.yml, lenses-cli.yml or lenses.json, lenses.yml and lenses.tml.
func TryReadConfigurationFromExecutable(outPtr *Configuration) bool {
	executablePath, err := os.Executable()
	if err != nil {
		return false
	}

	executablePath = filepath.Dir(executablePath)

	return lookupConfiguration(executablePath, outPtr)
}

// TryReadConfigurationFromCurrentWorkingDir will try to read the `Configuration`
// from the current working directory, note that it may differs from the executable path.
// The lookup is based on the common configuration filename pattern:
// lenses-cli.json, lenses-cli.yml, lenses-cli.yml or lenses.json, lenses.yml and lenses.tml.
func TryReadConfigurationFromCurrentWorkingDir(outPtr *Configuration) bool {
	workingDir, err := os.Getwd()
	if err != nil {
		return false
	}

	return lookupConfiguration(workingDir, outPtr)
}

// ReadConfigurationFromJSON reads and decodes Configuration from a json file, i.e `configuration.json`.
//
// Accepts the absolute or the relative path of the configuration file.
// Parsing error will result to a panic.
// Error may occur when the file doesn't exists or is not formatted correctly.
func ReadConfigurationFromJSON(filename string, outPtr *Configuration) error {
	return ReadConfigurationFromFile(filename, ConfigurationUnmarshalJSON, outPtr)
}

// ReadConfigurationFromYAML reads and decodes Configuration from a yaml file, i.e `configuration.yml`.
//
// Accepts the absolute or the relative path of the configuration file.
// Parsing error will result to a panic.
// Error may occur when the file doesn't exists or is not formatted correctly.
func ReadConfigurationFromYAML(filename string, outPtr *Configuration) error {
	return ReadConfigurationFromFile(filename, ConfigurationUnmarshalYAML, outPtr)
}
