//go:build harness

package daemon

import "github.com/Zovaris/smithy/internal/api"

// The wire types and client live in internal/api; the aliases keep daemon code short.
type (
	AgentChoice       = api.AgentChoice
	BusStartReq       = api.BusStartReq
	ConnectTestResult = api.ConnectTestResult
	Decision          = api.Decision
	DirList           = api.DirList
	KeyRow            = api.KeyRow
	KitChoice         = api.KitChoice
	LinkIssue         = api.LinkIssue
	LinkURL           = api.LinkURL
	Mail              = api.Mail
	Outcome           = api.Outcome
	Project           = api.Project
	ProjectList       = api.ProjectList
	PromptState       = api.PromptState
	ScheduleDetail    = api.ScheduleDetail
	ScheduleList      = api.ScheduleList
	Session           = api.Session
	SessionLinks      = api.SessionLinks
	SessionList       = api.SessionList
	SettingRow        = api.SettingRow
	Settings          = api.Settings
	Setup             = api.Setup
	State             = api.State
	TaskBoard         = api.TaskBoard
	TaskCard          = api.TaskCard
	TaskList          = api.TaskList
	TendList          = api.TendList
	TendPR            = api.TendPR
	TendRef           = api.TendRef
	WorkflowDetail    = api.WorkflowDetail
	WorkflowList      = api.WorkflowList
	Client            = api.Client
	ClientMsg         = api.ClientMsg
	BusClient         = api.BusClient
)
