package wps

// ResetPasswordStatusResponse is the top-level response for GET /members/reset-password-status.
//
// Example:
//
//	{
//	  "success": true,
//	  "value": {
//	    "isEmailResetEnabled": true,
//	    "isSmsResetEnabled": false
//	  }
//	}
type ResetPasswordStatusResponse struct {
	Value   ResetPasswordStatusValue `json:"value"`
	Success bool                     `json:"success"`
}

// ResetPasswordStatusValue holds the password-reset channel flags.
type ResetPasswordStatusValue struct {
	IsEmailResetEnabled bool `json:"isEmailResetEnabled"`
	IsSmsResetEnabled   bool `json:"isSmsResetEnabled"`
}
