package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"regexp"
	"strings"

	"github.com/Venafi/vcert/pkg/certificate"
)

type Command interface {
	validateFlags() error
	prepFlags()
	execute() error
}

func parseCommand() (Command, error) {
	// don't use the logger until it has been setup to write to file
	log.SetOutput(&NoopWriter{})

	if len(os.Args) < 2 {
		return &HelpCommand{}, nil
	}

	command := os.Args[1]

	var v Command
	switch command {
	case "create":
		v = &GenerateAndStoreCommand{}
	case "login":
		v = &LoginCommand{}
	case "delete":
		v = &DeleteCommand{}
	case "list":
		v = &ListCommand{}
	default:
		return nil, fmt.Errorf("command not recognized %s", command)
	}

	newArgs := []string{os.Args[0]}
	newArgs = append(newArgs, os.Args[2:]...)
	os.Args = newArgs

	v.prepFlags()

	flag.Parse()
	err := v.validateFlags()
	if err != nil {
		return nil, err
	}

	return v, nil
}

type ListCommand struct {
	ByThumbprint  bool
	ByCommonName  bool
	ByPath        bool
	VenafiPrefix  string
	CredhubPrefix string
	VenafiRoot    string
	CredhubRoot   string
	VenafiLimit   int
}

func (v *ListCommand) validateFlags() error {
	return nil
}

func (v *ListCommand) prepFlags() {
	flag.BoolVar(&v.ByThumbprint, "bythumbprint", false, "Compare by thumbprint. Note this will be slower due to the need to download each cert from CredHub.")
	flag.BoolVar(&v.ByCommonName, "bycommonname", false, "Compare by certificate common name from Venafi and file basename on the CredHub side.")
	flag.BoolVar(&v.ByPath, "bypath", false, "Compare by path")
	flag.StringVar(&v.VenafiPrefix, "vprefix", "", "Venafi prefix to strip from returned values")
	flag.StringVar(&v.CredhubPrefix, "cprefix", "", "Credhub prefix to strip from returned values")
	flag.StringVar(&v.VenafiRoot, "vroot", "", "Subpath to search in Venafi")
	flag.StringVar(&v.CredhubRoot, "croot", "", "Subpath to search in CredHub")
	flag.IntVar(&v.VenafiLimit, "vlimit", 100, "(Default 100) Limits the number of Venafi results returned")
}

func (v *ListCommand) execute() error {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configYAML, err := readConfigFile(userHomeDir, ConfigFile)
	if err != nil {
		return err
	}

	configLoader := ConfigLoader{
		userHomeDir:    userHomeDir,
		cvConfigDir:    ".cv",
		configFilename: "config.json",
	}
	config, err := configLoader.readConfig()
	if err != nil {
		return err
	}

	if v.VenafiRoot == "" && configYAML.VcertZone != "" {
		v.VenafiRoot = prependPolicyRoot(configYAML.VcertZone)
	}

	cp := &CredhubProxy{
		baseUrl:           config.CredhubBaseUrl,
		accessToken:       config.AccessToken,
		refreshToken:      config.RefreshToken,
		authUrl:           config.AuthUrl,
		skipTLSValidation: config.SkipTLSValidation,
		clientId:          configYAML.ClientID,
		clientSecret:      configYAML.ClientSecret,
		configPath:        ".cv",
	}

	cv := CV{
		configLoader: configLoader,
		credhub:      cp,
		vcert: &VcertProxy{
			username:      configYAML.VcertUsername,
			password:      configYAML.VcertPassword,
			zone:          configYAML.VcertZone,
			baseUrl:       configYAML.VcertBaseUrl,
			connectorType: configYAML.ConnectorType,
		},
	}

	err = cp.authExisting()
	if err != nil {
		return err
	}

	err = cv.vcert.login()
	if err != nil {
		return err
	}
	_, err = cv.listBoth(v)
	return err
}

type GenerateAndStoreCommand struct {
	Name string

	CommonName         string                    // v,c
	SANDNS             stringSlice               // v
	KeyType            certificate.KeyType       // v
	KeyCurve           certificate.EllipticCurve // v
	OrganizationName   string                    // v,c
	OrganizationalUnit stringSlice               // v,c
	Country            string                    // v,c
	State              string                    // v,c
	Locality           string                    // v,c
	SANEmail           emailSlice                // v
	SANIP              ipSlice                   // v
	KeyPassword        string                    // v
	AuthConfig         CVConfig                  // v

	// -n, --name=              Name of the credential to generate
	// Name string // c

	// CommonName         string
	// SANDNS             stringSlice
	// KeyType            certificate.KeyType
	// KeyCurve           certificate.EllipticCurve
	// OrganizationName   string
	// OrganizationalUnit stringSlice
	// Country            string
	// State              string
	// Locality           string
	// SANEmail           emailSlice
	// SANIP              ipSlice
	// KeyPassword        string
	// AuthConfig         CVConfig

	// -t, --type=              Sets the credential type to generate. Valid types include 'password', 'user', 'certificate', 'ssh' and 'rsa'.
	// -O, --no-overwrite       Credential is not modified if stored value already exists
	NoOverwrite bool // c
	// -j, --output-json        Return response in JSON format
	// -k, --key-length=        [Certificate, SSH, RSA] Bit length of the generated key (Default: 2048)
	KeyLength int // c
	// -d, --duration=          [Certificate] Valid duration (in days) of the generated certificate (Default: 365)
	Duration int // c
	// -c, --common-name=       [Certificate] Common name of the generated certificate
	// CommonName string // c
	// -o, --organization=      [Certificate] OrganizationName of the generated certificate
	// OrganizationName string // c
	// -u, --organization-unit= [Certificate] Organization unit of the generated certificate
	// OrganizationalUnit string // c
	// -i, --locality=          [Certificate] Locality/city of the generated certificate
	// Locality string // c
	// -s, --state=             [Certificate] State/province of the generated certificate
	// State string // c
	// -y, --country=           [Certificate] Country of the generated certificate
	// Country string // c
	// -a, --alternative-name=  [Certificate] A subject alternative name of the generated certificate (may be specified multiple times)
	AlternativeName stringSlice // c
	// -g, --key-usage=         [Certificate] Key Usage extensions for the generated certificate (may be specified multiple times)
	KeyUsage stringSlice // c
	// -e, --ext-key-usage=     [Certificate] Extended Key Usage extensions for the generated certificate (may be specified multiple times)
	ExtKeyUsage stringSlice // c
	// --ca=                [Certificate] Name of CA used to sign the generated certificate
	CA string // c
	// --is-ca              [Certificate] The generated certificate is a certificate authority
	IsCA bool // c
	// --self-sign          [Certificate] The generated certificate will be self-signed
	SelfSign bool // c

	GenOnly bool
	Credhub bool
}

func (v *GenerateAndStoreCommand) validateFlags() error {
	if v.CommonName == "" && len(v.SANDNS) == 0 {
		return errors.New("You must have a common name or san-dns")
	}
	return nil
}

func (v *GenerateAndStoreCommand) prepFlags() {
	flag.StringVar(&v.Name, "name", "", "Credhub Name")

	flag.StringVar(&v.CommonName, "cn", "", "(all) Common name")
	flag.Var(&v.SANDNS, "san-dns", "(Venafi) San DNS")
	flag.Var(&v.KeyType, "key-type", "(Venafi) Key type")
	flag.Var(&v.KeyCurve, "key-curve", "(Venafi) Key curve")
	flag.StringVar(&v.OrganizationName, "o", "", "(all) Organization Name")
	flag.Var(&v.OrganizationalUnit, "ou", "(all) Organizational Unit")
	flag.StringVar(&v.Country, "c", "", "(all) Country")
	flag.StringVar(&v.State, "st", "", "(all) State")
	flag.StringVar(&v.Locality, "l", "", "(all) Locality")
	flag.Var(&v.SANEmail, "san-email", "(Venafi) SAN Email")
	flag.Var(&v.SANIP, "san-ip", "(Venafi) SAN IP")
	flag.StringVar(&v.KeyPassword, "key-password", "", "(Venafi) Key Password")

	// -O, --no-overwrite       Credential is not modified if stored value already exists
	flag.BoolVar(&v.NoOverwrite, "no-overwrite", false, "(CredHub) NoOverwrite")
	// -k, --key-length=        [Certificate, SSH, RSA] Bit length of the generated key (Default: 2048)
	flag.IntVar(&v.KeyLength, "key-length", 2048, "(CredHub) KeyLength")
	// -d, --duration=          [Certificate] Valid duration (in days) of the generated certificate (Default: 365)
	flag.IntVar(&v.Duration, "duration", 365, "(CredHub) Duration")
	// -a, --alternative-name=  [Certificate] A subject alternative name of the generated certificate (may be specified multiple times)
	flag.Var(&v.AlternativeName, "alternative-name", "(CredHub) AlternativeName")
	// -g, --key-usage=         [Certificate] Key Usage extensions for the generated certificate (may be specified multiple times)
	flag.Var(&v.KeyUsage, "key-usage", "(CredHub) KeyUsage")
	// -e, --ext-key-usage=     [Certificate] Extended Key Usage extensions for the generated certificate (may be specified multiple times)
	flag.Var(&v.ExtKeyUsage, "ext-key-usage", "(CredHub) ExtKeyUsage")
	// --ca=                [Certificate] Name of CA used to sign the generated certificate
	flag.StringVar(&v.CA, "ca", "", "(CredHub) CA")
	// --is-ca              [Certificate] The generated certificate is a certificate authority
	flag.BoolVar(&v.IsCA, "is-ca", false, "(CredHub) IsCA")
	// --self-sign          [Certificate] The generated certificate will be self-signed
	flag.BoolVar(&v.SelfSign, "self-sign", false, "(CredHub) SelfSign")

	flag.BoolVar(&v.GenOnly, "genonly", false, "(all) Only generate the cert. Do not copy it to the other platform. By default cert is copied from generated platform to other platform.")
	flag.BoolVar(&v.Credhub, "credhub", false, "(CredHub) Generate the certificate on the CredHub platform. By default the certificate is generated on the Venafi platform.")
}

func (v *GenerateAndStoreCommand) execute() error {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configYAML, err := readConfigFile(userHomeDir, ConfigFile)
	if err != nil {
		return err
	}

	configLoader := ConfigLoader{
		userHomeDir:    userHomeDir,
		cvConfigDir:    ".cv",
		configFilename: "config.json",
	}
	config, err := configLoader.readConfig()
	if err != nil {
		return err
	}

	cp := &CredhubProxy{
		baseUrl:           config.CredhubBaseUrl,
		accessToken:       config.AccessToken,
		refreshToken:      config.RefreshToken,
		authUrl:           config.AuthUrl,
		skipTLSValidation: config.SkipTLSValidation,
		clientId:          configYAML.ClientID,
		clientSecret:      configYAML.ClientSecret,
		configPath:        ".cv",
	}

	cv := CV{
		configLoader: configLoader,
		credhub:      cp,
		vcert: &VcertProxy{
			username:      configYAML.VcertUsername,
			password:      configYAML.VcertPassword,
			zone:          configYAML.VcertZone,
			baseUrl:       configYAML.VcertBaseUrl,
			connectorType: configYAML.ConnectorType,
		},
	}
	err = cp.authExisting()
	if err != nil {
		return err
	}

	err = cv.vcert.login()
	if err != nil {
		return err
	}

	if v.Name == "" {
		v.Name = v.CommonName
	}

	if v.Credhub {
		return cv.generateAndStoreCredhub(v.Name, v, !v.GenOnly)
	} else {
		return cv.generateAndStore(v.Name, v, !v.GenOnly)
	}
}

type LoginCommand struct {
	Username          string
	Password          string
	CredhubBaseUrl    string
	ClientId          string
	ClientSecret      string
	SkipTLSValidation bool
	configYAML        *YAMLConfig
}

func (v *LoginCommand) validateFlags() error {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	v.configYAML, err = readConfigFile(userHomeDir, ConfigFile)
	if err != nil {
		return err
	}

	if v.configYAML.CredhubUsername != "" && v.Username == "" {
		v.Username = v.configYAML.CredhubUsername
	}

	if v.configYAML.CredhubPassword != "" && v.Password == "" {
		v.Password = v.configYAML.CredhubPassword
	}

	if v.configYAML.ClientID != "" && v.ClientId == "" {
		v.ClientId = v.configYAML.ClientID
	}

	if v.configYAML.ClientSecret != "" && v.ClientSecret == "" {
		v.ClientSecret = v.configYAML.ClientSecret
	}

	if v.configYAML.CredhubEndpoint != "" && v.CredhubBaseUrl == "" {
		v.CredhubBaseUrl = v.configYAML.CredhubEndpoint
	}

	if v.configYAML.SkipTLSValidation && !v.SkipTLSValidation {
		v.SkipTLSValidation = true
	}

	if v.Username == "" && v.ClientId == "" {
		return fmt.Errorf("Username or clientid is required")
	}

	if v.CredhubBaseUrl == "" {
		return fmt.Errorf("Credhub endpoint url is required")
	}

	return nil
}

func (v *LoginCommand) prepFlags() {
	flag.StringVar(&v.Username, "u", "", "Username")
	flag.StringVar(&v.Password, "p", "", "Password")
	flag.StringVar(&v.CredhubBaseUrl, "url", "", "Credhub Base Url")
	flag.StringVar(&v.ClientId, "clientid", "", "Client Id")
	flag.StringVar(&v.ClientSecret, "clientsecret", "", "Client Secret")
	flag.BoolVar(&v.SkipTLSValidation, "skip-tls-validation", false, "Skip tls validation for test purposes")
}

func (v *LoginCommand) execute() error {
	cp := &CredhubProxy{
		baseUrl:           v.CredhubBaseUrl,
		username:          v.Username,
		password:          v.Password,
		clientId:          v.ClientId,
		clientSecret:      v.ClientSecret,
		skipTLSValidation: v.SkipTLSValidation,
		configPath:        ".cv",
	}
	err := cp.auth()
	if err == nil {
		status("Login Successful\n")
	}
	return err
}

type HelpCommand struct {
}

func (v *HelpCommand) validateFlags() error {
	return nil
}

func (v *HelpCommand) prepFlags() {
}

func (v *HelpCommand) execute() error {
	helpoutput(
		`Usage:
  cv [command]

Available commands:
  login              Log in to CredHub
  create             Generate a credential and upload to counterpart system
  list               List credentials in each system
  delete             Delete a credential
`)
	return nil
}

type DeleteCommand struct {
	Name string
}

func (v *DeleteCommand) validateFlags() error {
	if v.Name == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

func (v *DeleteCommand) prepFlags() {
	flag.StringVar(&v.Name, "name", "", "Name")
}

func (v *DeleteCommand) execute() error {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	configYAML, err := readConfigFile(userHomeDir, ConfigFile)
	if err != nil {
		return err
	}

	configLoader := ConfigLoader{
		userHomeDir:    userHomeDir,
		cvConfigDir:    ".cv",
		configFilename: "config.json",
	}
	config, err := configLoader.readConfig()
	if err != nil {
		return err
	}

	cp := &CredhubProxy{
		baseUrl:           config.CredhubBaseUrl,
		accessToken:       config.AccessToken,
		refreshToken:      config.RefreshToken,
		authUrl:           config.AuthUrl,
		skipTLSValidation: config.SkipTLSValidation,
		clientId:          configYAML.ClientID,
		clientSecret:      configYAML.ClientSecret,
		configPath:        ".cv",
	}

	cv := CV{
		configLoader: configLoader,
		credhub:      cp,
		vcert: &VcertProxy{
			username:      configYAML.VcertUsername,
			password:      configYAML.VcertPassword,
			zone:          configYAML.VcertZone,
			baseUrl:       configYAML.VcertBaseUrl,
			connectorType: configYAML.ConnectorType,
		},
	}

	err = cp.authExisting()
	if err != nil {
		return err
	}

	err = cv.vcert.login()
	if err != nil {
		return err
	}
	return cv.deleteCert(v.Name)
}

type NoopWriter struct {
}

func (w *NoopWriter) Write(p []byte) (n int, err error) {
	return 0, nil
}

type stringSlice []string

func (ss *stringSlice) String() string {
	if len(*ss) == 0 {
		return ""
	}
	return strings.Join(*ss, "\n") + "\n"
}

func (ss *stringSlice) Set(value string) error {
	*ss = append(*ss, value)
	return nil
}

type ipSlice []net.IP

func (is *ipSlice) String() string {
	var ret string
	for _, s := range *is {
		ret += fmt.Sprintf("%s\n", s)
	}
	return ret
}

func (is *ipSlice) Set(value string) error {
	temp := net.ParseIP(value)
	if temp != nil {
		*is = append(*is, temp)
		return nil
	}
	return fmt.Errorf("Failed to convert %s to an IP Address", value)
}

type emailSlice []string

func (es *emailSlice) String() string {
	var ret string
	for _, s := range *es {
		ret += fmt.Sprintf("%s\n", s)
	}
	return ret
}

func (es *emailSlice) Set(value string) error {
	if isValidEmailAddress(value) {
		*es = append(*es, value)
		return nil
	}
	return fmt.Errorf("Failed to convert %s to an Email Address", value)
}

const emailRegex = "[[:alnum:]][\\w\\.-]*[[:alnum:]]@[[:alnum:]][\\w\\.-]*[[:alnum:]]\\.[[:alpha:]][a-z\\.]*[[:alpha:]]$"

func isValidEmailAddress(email string) bool {
	reg := regexp.MustCompile(emailRegex)
	return reg.FindStringIndex(email) != nil
}
