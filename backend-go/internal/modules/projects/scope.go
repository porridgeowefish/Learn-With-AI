package projects

import scope "github.com/xmz14/lll/backend-go/internal/modules/projects/internal/learningscope"

const SchemaVersion = scope.SchemaVersion
const StatusDraft = scope.StatusDraft
const StatusReady = scope.StatusReady
const SourceStandalone = scope.SourceStandalone
const SourceDisciplineMap = scope.SourceDisciplineMap

type Source = scope.Source
type Topic = scope.Topic
type Catalog = scope.Catalog
type Scope = scope.Scope

var NewDraft = scope.NewDraft
var SnapshotFromTopic = scope.SnapshotFromTopic
var ReadCatalog = scope.ReadCatalog
var MarshalScope = scope.MarshalScope
var EmptyCatalog = scope.EmptyCatalog
var FindTopic = scope.FindTopic
var ValidateCatalog = scope.ValidateCatalog
var ValidateAgainstOverview = scope.ValidateAgainstOverview
