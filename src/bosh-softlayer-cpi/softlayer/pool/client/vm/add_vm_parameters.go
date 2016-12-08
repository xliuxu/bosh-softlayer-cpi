package vm

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"time"

	"golang.org/x/net/context"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"

	strfmt "github.com/go-openapi/strfmt"

	"bosh-softlayer-cpi/softlayer/pool/models"
)

// NewAddVMParams creates a new AddVMParams object
// with the default values initialized.
func NewAddVMParams() *AddVMParams {
	var ()
	return &AddVMParams{

		timeout: cr.DefaultTimeout,
	}
}

// NewAddVMParamsWithTimeout creates a new AddVMParams object
// with the default values initialized, and the ability to set a timeout on a request
func NewAddVMParamsWithTimeout(timeout time.Duration) *AddVMParams {
	var ()
	return &AddVMParams{

		timeout: timeout,
	}
}

// NewAddVMParamsWithContext creates a new AddVMParams object
// with the default values initialized, and the ability to set a context for a request
func NewAddVMParamsWithContext(ctx context.Context) *AddVMParams {
	var ()
	return &AddVMParams{

		Context: ctx,
	}
}

/*AddVMParams contains all the parameters to send to the API endpoint
for the add Vm operation typically these are written to a http.Request
*/
type AddVMParams struct {

	/*Body
	  Vm object that needs to be added to the pool

	*/
	Body *models.VM

	timeout time.Duration
	Context context.Context
}

// WithTimeout adds the timeout to the add Vm params
func (o *AddVMParams) WithTimeout(timeout time.Duration) *AddVMParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the add Vm params
func (o *AddVMParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the add Vm params
func (o *AddVMParams) WithContext(ctx context.Context) *AddVMParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the add Vm params
func (o *AddVMParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithBody adds the body to the add Vm params
func (o *AddVMParams) WithBody(body *models.VM) *AddVMParams {
	o.SetBody(body)
	return o
}

// SetBody adds the body to the add Vm params
func (o *AddVMParams) SetBody(body *models.VM) {
	o.Body = body
}

// WriteToRequest writes these params to a swagger request
func (o *AddVMParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	r.SetTimeout(o.timeout)
	var res []error

	if o.Body == nil {
		o.Body = new(models.VM)
	}

	if err := r.SetBodyParam(o.Body); err != nil {
		return err
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
