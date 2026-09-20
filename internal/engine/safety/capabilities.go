package safety

// Capability describes a concrete effect a tool may have. Policies should
// reason about capabilities instead of relying on tool-name allowlists.
type Capability string

// RiskLevel is the default severity associated with a tool capability set.
type RiskLevel string

const (
	RiskLow    RiskLevel = "low"
	RiskMedium RiskLevel = "medium"
	RiskHigh   RiskLevel = "high"
)

const (
	CapabilityUnknown            Capability = "unknown"
	CapabilityFilesystemRead     Capability = "filesystem.read"
	CapabilityFilesystemWrite    Capability = "filesystem.write"
	CapabilityFilesystemDelete   Capability = "filesystem.delete"
	CapabilityProcessExecute     Capability = "process.execute"
	CapabilityNetworkAccess      Capability = "network.access"
	CapabilityCredentialsAccess  Capability = "credentials.access"
	CapabilityUserInteraction    Capability = "user.interaction"
	CapabilitySpecRead           Capability = "spec.read"
	CapabilitySpecWrite          Capability = "spec.write"
	CapabilitySpecApprove        Capability = "spec.approve"
	CapabilityConfigurationRead  Capability = "configuration.read"
	CapabilityConfigurationWrite Capability = "configuration.write"
	CapabilityDestructive        Capability = "destructive"
)

// ToolPolicy is the declarative safety metadata for a canonical tool.
type ToolPolicy struct {
	Name         string
	Capabilities []Capability
	DefaultRisk  RiskLevel
}

var toolPolicies = map[string]ToolPolicy{
	"Read":                  {Name: "Read", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"Glob":                  {Name: "Glob", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"Grep":                  {Name: "Grep", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"LS":                    {Name: "LS", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"ToolHealth":            {Name: "ToolHealth", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"ToolSearch":            {Name: "ToolSearch", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"SessionQuery":          {Name: "SessionQuery", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"TaskOutput":            {Name: "TaskOutput", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"WaitTasks":             {Name: "WaitTasks", Capabilities: []Capability{CapabilityUserInteraction}, DefaultRisk: RiskLow},
	"Outline":               {Name: "Outline", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"SmartRead":             {Name: "SmartRead", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"CodeSearch":            {Name: "CodeSearch", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"CodeMatch":             {Name: "CodeMatch", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"FuzzyFind":             {Name: "FuzzyFind", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"BatchExec":             {Name: "BatchExec", Capabilities: []Capability{CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"Toolset":               {Name: "Toolset", Capabilities: nil, DefaultRisk: RiskLow},
	"CodeGraph":             {Name: "CodeGraph", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"Impact":                {Name: "Impact", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"GitHistory":            {Name: "GitHistory", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute}, DefaultRisk: RiskLow},
	"Diagnostics":           {Name: "Diagnostics", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"Skill":                 {Name: "Skill", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityFilesystemWrite, CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"Agent":                 {Name: "Agent", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"TodoWrite":             {Name: "TodoWrite", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"TaskStop":              {Name: "TaskStop", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"KillTask":              {Name: "KillTask", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"Monitor":               {Name: "Monitor", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"MultiEdit":             {Name: "MultiEdit", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"StructuredEdit":        {Name: "StructuredEdit", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"AskUserQuestion":       {Name: "AskUserQuestion", Capabilities: []Capability{CapabilityUserInteraction}, DefaultRisk: RiskLow},
	"RequestCredential":     {Name: "RequestCredential", Capabilities: []Capability{CapabilityCredentialsAccess, CapabilityUserInteraction}, DefaultRisk: RiskHigh},
	"TerminalCreate":        {Name: "TerminalCreate", Capabilities: []Capability{CapabilityProcessExecute, CapabilityDestructive}, DefaultRisk: RiskHigh},
	"TerminalSend":          {Name: "TerminalSend", Capabilities: []Capability{CapabilityProcessExecute, CapabilityDestructive}, DefaultRisk: RiskHigh},
	"TerminalRead":          {Name: "TerminalRead", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"TerminalList":          {Name: "TerminalList", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"TerminalResize":        {Name: "TerminalResize", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"TerminalKill":          {Name: "TerminalKill", Capabilities: []Capability{CapabilityProcessExecute, CapabilityDestructive}, DefaultRisk: RiskHigh},
	"ProjectVerify":         {Name: "ProjectVerify", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"AppVerify":             {Name: "AppVerify", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"GenerateMedia":         {Name: "GenerateMedia", Capabilities: []Capability{CapabilityNetworkAccess, CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"DependencyAudit":       {Name: "DependencyAudit", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute, CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"Git":                   {Name: "Git", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"GitHub":                {Name: "GitHub", Capabilities: []Capability{CapabilityNetworkAccess, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"WebFetch":              {Name: "WebFetch", Capabilities: []Capability{CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"WebSearch":             {Name: "WebSearch", Capabilities: []Capability{CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"Browser":               {Name: "Browser", Capabilities: []Capability{CapabilityNetworkAccess, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"Screenshot":            {Name: "Screenshot", Capabilities: []Capability{CapabilityNetworkAccess, CapabilityFilesystemWrite}, DefaultRisk: RiskHigh},
	"Download":              {Name: "Download", Capabilities: []Capability{CapabilityNetworkAccess, CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"Bash":                  {Name: "Bash", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"PowerShell":            {Name: "PowerShell", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"LSP":                   {Name: "LSP", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"Write":                 {Name: "Write", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"Edit":                  {Name: "Edit", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"Delete":                {Name: "Delete", Capabilities: []Capability{CapabilityFilesystemDelete, CapabilityDestructive}, DefaultRisk: RiskHigh},
	"Specify":               {Name: "Specify", Capabilities: []Capability{CapabilitySpecWrite}, DefaultRisk: RiskLow},
	"Plan":                  {Name: "Plan", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskLow},
	"Tasks":                 {Name: "Tasks", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskLow},
	"ApproveImplementation": {Name: "ApproveImplementation", Capabilities: []Capability{CapabilitySpecApprove, CapabilityUserInteraction}, DefaultRisk: RiskHigh},
	"SpecStatus":            {Name: "SpecStatus", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"SpecList":              {Name: "SpecList", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"SpecEdit":              {Name: "SpecEdit", Capabilities: []Capability{CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecReset":             {Name: "SpecReset", Capabilities: []Capability{CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecConfig":            {Name: "SpecConfig", Capabilities: []Capability{CapabilityConfigurationRead, CapabilityConfigurationWrite}, DefaultRisk: RiskMedium},
	"Clarify":               {Name: "Clarify", Capabilities: []Capability{CapabilitySpecRead, CapabilityUserInteraction}, DefaultRisk: RiskLow},
	"Analyze":               {Name: "Analyze", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"Checklist":             {Name: "Checklist", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"Constitution":          {Name: "Constitution", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"Converge":              {Name: "Converge", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecAdaptive":          {Name: "SpecAdaptive", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecAdr":               {Name: "SpecAdr", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecBdd":               {Name: "SpecBdd", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecBlast":             {Name: "SpecBlast", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecDrift":             {Name: "SpecDrift", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"SpecGround":            {Name: "SpecGround", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecLinks":             {Name: "SpecLinks", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"SpecMaster":            {Name: "SpecMaster", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecParallel":          {Name: "SpecParallel", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecPlanVariations":    {Name: "SpecPlanVariations", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecProgress":          {Name: "SpecProgress", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"SpecProperties":        {Name: "SpecProperties", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"SpecProvenance":        {Name: "SpecProvenance", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecReview":            {Name: "SpecReview", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"SpecScale":             {Name: "SpecScale", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecSuper":             {Name: "SpecSuper", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecTestFirst":         {Name: "SpecTestFirst", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecTestGen":           {Name: "SpecTestGen", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"SpecTrace":             {Name: "SpecTrace", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"SpecVersion":           {Name: "SpecVersion", Capabilities: []Capability{CapabilitySpecRead}, DefaultRisk: RiskLow},
	"ScheduleCreate":        {Name: "ScheduleCreate", Capabilities: []Capability{CapabilityConfigurationWrite, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"ScheduleList":          {Name: "ScheduleList", Capabilities: []Capability{CapabilityConfigurationRead}, DefaultRisk: RiskLow},
	"ScheduleDelete":        {Name: "ScheduleDelete", Capabilities: []Capability{CapabilityConfigurationWrite, CapabilityDestructive}, DefaultRisk: RiskHigh},
	"Patch":                 {Name: "Patch", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"Batch":                 {Name: "Batch", Capabilities: []Capability{CapabilityFilesystemWrite, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"AtomicMultiEdit":       {Name: "AtomicMultiEdit", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskHigh},
	"AutoImport":            {Name: "AutoImport", Capabilities: []Capability{CapabilityFilesystemWrite, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"OrganizeImports":       {Name: "OrganizeImports", Capabilities: []Capability{CapabilityFilesystemWrite, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"Refactor":              {Name: "Refactor", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskHigh},
	"ResolveConflicts":      {Name: "ResolveConflicts", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskHigh},
	"Debug":                 {Name: "Debug", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"pr_generate":           {Name: "pr_generate", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"TasksToIssues":         {Name: "TasksToIssues", Capabilities: []Capability{CapabilitySpecRead, CapabilityNetworkAccess}, DefaultRisk: RiskHigh},
	"NotebookEdit":          {Name: "NotebookEdit", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"EnterWorktree":         {Name: "EnterWorktree", Capabilities: []Capability{CapabilityFilesystemWrite, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"ExitWorktree":          {Name: "ExitWorktree", Capabilities: []Capability{CapabilityFilesystemWrite, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"ListMcpResourcesTool":  {Name: "ListMcpResourcesTool", Capabilities: []Capability{CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"ReadMcpResourceTool":   {Name: "ReadMcpResourceTool", Capabilities: []Capability{CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"Config":                {Name: "Config", Capabilities: []Capability{CapabilityConfigurationRead, CapabilityConfigurationWrite}, DefaultRisk: RiskMedium},
	"SendUserMessage":       {Name: "SendUserMessage", Capabilities: []Capability{CapabilityUserInteraction}, DefaultRisk: RiskLow},
	"TaskCreate":            {Name: "TaskCreate", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"TaskGet":               {Name: "TaskGet", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"TaskList":              {Name: "TaskList", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"TaskUpdate":            {Name: "TaskUpdate", Capabilities: []Capability{CapabilityFilesystemWrite}, DefaultRisk: RiskMedium},
	"TaskRun":               {Name: "TaskRun", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"Sleep":                 {Name: "Sleep", Capabilities: nil, DefaultRisk: RiskLow},
	"CronCreate":            {Name: "CronCreate", Capabilities: []Capability{CapabilityConfigurationWrite, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"CronDelete":            {Name: "CronDelete", Capabilities: []Capability{CapabilityConfigurationWrite, CapabilityDestructive}, DefaultRisk: RiskHigh},
	"CronList":              {Name: "CronList", Capabilities: []Capability{CapabilityConfigurationRead}, DefaultRisk: RiskLow},
	"VerifyPlanExecution":   {Name: "VerifyPlanExecution", Capabilities: []Capability{CapabilityFilesystemRead}, DefaultRisk: RiskLow},
	"Workflow":              {Name: "Workflow", Capabilities: []Capability{CapabilitySpecRead, CapabilitySpecWrite}, DefaultRisk: RiskMedium},
	"McpAuth":               {Name: "McpAuth", Capabilities: []Capability{CapabilityCredentialsAccess, CapabilityNetworkAccess}, DefaultRisk: RiskHigh},
	"SearchX":               {Name: "SearchX", Capabilities: []Capability{CapabilityNetworkAccess}, DefaultRisk: RiskMedium},
	"ComputerUse":           {Name: "ComputerUse", Capabilities: []Capability{CapabilityProcessExecute, CapabilityNetworkAccess, CapabilityDestructive}, DefaultRisk: RiskHigh},
	"AgenticFetch":          {Name: "AgenticFetch", Capabilities: []Capability{CapabilityNetworkAccess, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"NilAway":               {Name: "NilAway", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"Revive":                {Name: "Revive", Capabilities: []Capability{CapabilityFilesystemRead, CapabilityProcessExecute}, DefaultRisk: RiskMedium},
	"MCPLSP":                {Name: "MCPLSP", Capabilities: []Capability{CapabilityNetworkAccess, CapabilityProcessExecute}, DefaultRisk: RiskHigh},
	"SQL":                   {Name: "SQL", Capabilities: []Capability{CapabilityNetworkAccess, CapabilityFilesystemRead, CapabilityFilesystemWrite}, DefaultRisk: RiskHigh},
	"Jobs":                  {Name: "Jobs", Capabilities: []Capability{CapabilityProcessExecute}, DefaultRisk: RiskMedium},
}

// ToolPolicyFor returns a copy of the policy for a canonical tool. Unknown
// tools are explicitly represented and therefore fail closed in strict policy.
func ToolPolicyFor(name string) ToolPolicy {
	canonical := canonicalToolName(name)
	if policy, ok := toolPolicies[canonical]; ok {
		policy.Capabilities = append([]Capability(nil), policy.Capabilities...)
		return policy
	}
	return ToolPolicy{Name: canonical, Capabilities: []Capability{CapabilityUnknown}, DefaultRisk: RiskHigh}
}

// ToolCapabilities returns a defensive copy of a tool's capabilities.
func ToolCapabilities(name string) []Capability {
	return ToolPolicyFor(name).Capabilities
}

func isKnownTool(name string) bool {
	policy := ToolPolicyFor(name)
	return len(policy.Capabilities) != 1 || policy.Capabilities[0] != CapabilityUnknown
}
