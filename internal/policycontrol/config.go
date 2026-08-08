package policycontrol

import "github.com/mrAndreyIsachenko/hexroute/internal/policyconfig"

const StaticConfigSchema = policyconfig.StaticConfigSchema

type StaticConfig = policyconfig.StaticConfig
type RuntimeConfig = policyconfig.RuntimeConfig

var ErrInvalidConfig = policyconfig.ErrInvalidConfig
