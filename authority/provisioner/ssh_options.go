package provisioner

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/pkg/errors"
	"go.step.sm/crypto/sshutil"
	"go.step.sm/linkedca"

	"github.com/smallstep/certificates/authority/policy"
)

// SSHCertificateOptions is an interface that returns a list of options passed when
// creating a new certificate.
type SSHCertificateOptions interface {
	Options(SignSSHOptions) []sshutil.Option
}

type sshCertificateOptionsFunc func(SignSSHOptions) []sshutil.Option

func (fn sshCertificateOptionsFunc) Options(so SignSSHOptions) []sshutil.Option {
	return fn(so)
}

// SSHOptions are a collection of custom options that can be added to each
// provisioner.
type SSHOptions struct {
	// Template contains an SSH certificate template. It can be a JSON template
	// escaped in a string or it can be also encoded in base64.
	Template string `json:"template,omitempty"`

	// TemplateFile points to a file containing a SSH certificate template.
	TemplateFile string `json:"templateFile,omitempty"`

	// TemplateData is a JSON object with variables that can be used in custom
	// templates.
	TemplateData json.RawMessage `json:"templateData,omitempty"`

	// User contains SSH user certificate options.
	User *policy.SSHUserCertificateOptions `json:"-"`

	// Host contains SSH host certificate options.
	Host *policy.SSHHostCertificateOptions `json:"-"`
}

// GetAllowedUserNameOptions returns the SSHNameOptions that are
// allowed when SSH User certificates are requested.
func (o *SSHOptions) GetAllowedUserNameOptions() *policy.SSHNameOptions {
	if o == nil {
		return nil
	}
	if o.User == nil {
		return nil
	}
	return o.User.AllowedNames
}

// GetDeniedUserNameOptions returns the SSHNameOptions that are
// denied when SSH user certificates are requested.
func (o *SSHOptions) GetDeniedUserNameOptions() *policy.SSHNameOptions {
	if o == nil {
		return nil
	}
	if o.User == nil {
		return nil
	}
	return o.User.DeniedNames
}

// GetAllowedHostNameOptions returns the SSHNameOptions that are
// allowed when SSH host certificates are requested.
func (o *SSHOptions) GetAllowedHostNameOptions() *policy.SSHNameOptions {
	if o == nil {
		return nil
	}
	if o.Host == nil {
		return nil
	}
	return o.Host.AllowedNames
}

// GetDeniedHostNameOptions returns the SSHNameOptions that are
// denied when SSH host certificates are requested.
func (o *SSHOptions) GetDeniedHostNameOptions() *policy.SSHNameOptions {
	if o == nil {
		return nil
	}
	if o.Host == nil {
		return nil
	}
	return o.Host.DeniedNames
}

// HasTemplate returns true if a template is defined in the provisioner options.
func (o *SSHOptions) HasTemplate() bool {
	return o != nil && (o.Template != "" || o.TemplateFile != "")
}

// TemplateSSHOptions generates a SSHCertificateOptions with the template and
// data defined in the ProvisionerOptions, the provisioner generated data, and
// the user data provided in the request. If no template has been provided,
// x509util.DefaultLeafTemplate will be used.
func TemplateSSHOptions(o *Options, data sshutil.TemplateData) (SSHCertificateOptions, error) {
	return CustomSSHTemplateOptions(o, data, sshutil.DefaultTemplate)
}

// CustomSSHTemplateOptions generates a CertificateOptions with the template, data
// defined in the ProvisionerOptions, the provisioner generated data and the
// user data provided in the request. If no template has been provided in the
// ProvisionerOptions, the given template will be used.
func CustomSSHTemplateOptions(o *Options, data sshutil.TemplateData, defaultTemplate string) (SSHCertificateOptions, error) {
	opts := o.GetSSHOptions()
	if data == nil {
		data = sshutil.NewTemplateData()
	}

	if opts != nil {
		// Add template data if any.
		if len(opts.TemplateData) > 0 && string(opts.TemplateData) != "null" {
			if err := json.Unmarshal(opts.TemplateData, &data); err != nil {
				return nil, errors.Wrap(err, "error unmarshaling template data")
			}
		}
	}

	return sshCertificateOptionsFunc(func(so SignSSHOptions) []sshutil.Option {
		var enrich = func(fn func(string, sshutil.TemplateData) sshutil.Option, arg1 string) sshutil.Option {
			return func(cr sshutil.CertificateRequest, sshOpts *sshutil.Options) error {
				if opts != nil {
					for _, wh := range o.Webhooks {
						if wh.Kind != linkedca.Webhook_ENRICHING.String() {
							continue
						}
						resp, err := wh.Do(context.Background(), so.WebhookClient, cr, data)
						if err != nil {
							return err
						}
						data.SetWebhook(wh.Name, resp.Data)
					}
				}
				return fn(arg1, data)(cr, sshOpts)
			}
		}

		// We're not provided user data without custom templates.
		if !opts.HasTemplate() {
			return []sshutil.Option{
				enrich(sshutil.WithTemplate, defaultTemplate),
			}
		}

		// Add user provided data.
		if len(so.TemplateData) > 0 {
			userObject := make(map[string]interface{})
			if err := json.Unmarshal(so.TemplateData, &userObject); err != nil {
				data.SetUserData(map[string]interface{}{})
			} else {
				data.SetUserData(userObject)
			}
		}

		// Load a template from a file if Template is not defined.
		if opts.Template == "" && opts.TemplateFile != "" {
			return []sshutil.Option{
				enrich(sshutil.WithTemplateFile, opts.TemplateFile),
			}
		}

		// Load a template from the Template fields
		// 1. As a JSON in a string.
		template := strings.TrimSpace(opts.Template)
		if strings.HasPrefix(template, "{") {
			return []sshutil.Option{
				enrich(sshutil.WithTemplate, template),
			}
		}
		// 2. As a base64 encoded JSON.
		return []sshutil.Option{
			enrich(sshutil.WithTemplateBase64, template),
		}
	}), nil
}
