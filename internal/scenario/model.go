package scenario

import "time"

type CommandType string

const (
	CommandSend     CommandType = "send"
	CommandRecv     CommandType = "recv"
	CommandSendCmd  CommandType = "sendCmd"
	CommandRecvCmd  CommandType = "recvCmd"
	CommandPause    CommandType = "pause"
	CommandNop      CommandType = "nop"
	CommandLabel    CommandType = "label"
	CommandTimeWait CommandType = "timewait"
)

type Mode string

const (
	ModeClient Mode = "client"
	ModeServer Mode = "server"
)

type Scenario struct {
	Name            string
	BasePath        string
	Mode            Mode
	Commands        []Command
	InitCommands    []Command
	Labels          map[string]int
	InitLabels      map[string]int
	GlobalVariables []string
	UserVariables   []string
	References      []string
}

type ActionType string

const (
	ActionEReg         ActionType = "ereg"
	ActionAssignStr    ActionType = "assignstr"
	ActionAssign       ActionType = "assign"
	ActionToDouble     ActionType = "todouble"
	ActionAdd          ActionType = "add"
	ActionSubtract     ActionType = "subtract"
	ActionMultiply     ActionType = "multiply"
	ActionDivide       ActionType = "divide"
	ActionStrCmp       ActionType = "strcmp"
	ActionTest         ActionType = "test"
	ActionLog          ActionType = "log"
	ActionWarning      ActionType = "warning"
	ActionLookup       ActionType = "lookup"
	ActionJump         ActionType = "jump"
	ActionGetTimeOfDay ActionType = "gettimeofday"
	ActionURLEncode    ActionType = "urlencode"
	ActionURLDecode    ActionType = "urldecode"
	ActionVerifyAuth   ActionType = "verifyauth"
	ActionExec         ActionType = "exec"
	ActionSetDest      ActionType = "setdest"
)

type Action struct {
	Type           ActionType
	Regexp         string
	SearchIn       string
	Header         string
	Variable       string
	Variable2      string
	AssignTo       []string
	CheckIt        bool
	CheckItInverse bool
	Value          string
	Compare        string
	Message        string
	File           string
	Key            string
	Username       string
	Password       string
	Command        string
	IntCmd         string
	RTPStream      string
	RTPCheck       string
	PlayPCAPAudio  string
	PlayPCAPVideo  string
	PlayPCAPImage  string
	Host           string
	Port           string
	Protocol       string
}

type Command struct {
	Type            CommandType
	Index           int
	Display         string
	Counter         string
	StartRTD        string
	StopRTD         string
	NextLabel       string
	NextIndex       int
	Test            string
	Chance          float64
	CondExec        string
	CondExecInverse bool

	SendText string
	Retrans  time.Duration
	RecvReq  string
	RecvResp string
	RRS      bool
	CmdDest  string
	CmdSrc   string
	Optional bool
	Timeout  time.Duration
	Pause    time.Duration
	LabelID  string
	TimeWait bool
	Actions  []Action
}

func (c Command) IsReceive() bool {
	return c.Type == CommandRecv || c.Type == CommandRecvCmd
}

func (c Command) IsSend() bool {
	return c.Type == CommandSend || c.Type == CommandSendCmd
}
