package registry

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/stoicsoft/vpsbox/internal/config"
)

const currentVersion = 2

// bucketsVersion is the schema version of buckets.json. It is tracked separately
// from currentVersion because buckets.json is its own file with its own shape —
// bumping the instance registry's version must not imply a bucket migration.
const bucketsVersion = 1

// workspacesVersion is the schema version of workspaces.json, tracked separately
// for the same reason bucketsVersion is.
const workspacesVersion = 1

type Instance struct {
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	Host             string    `json:"host"`
	Hostname         string    `json:"hostname,omitempty"`
	Port             int       `json:"port"`
	Username         string    `json:"username"`
	PrivateKeyPath   string    `json:"private_key_path"`
	PublicKeyPath    string    `json:"public_key_path,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	Image            string    `json:"image"`
	Labels           []string  `json:"labels,omitempty"`
	Backend          string    `json:"backend,omitempty"`
	CPUs             int       `json:"cpus,omitempty"`
	MemoryGB         int       `json:"memory_gb,omitempty"`
	DiskGB           int       `json:"disk_gb,omitempty"`
	CertPath         string    `json:"cert_path,omitempty"`
	CertKeyPath      string    `json:"cert_key_path,omitempty"`
	SandboxMarker    string    `json:"sandbox_marker,omitempty"`
	QuickTunnelURL   string    `json:"quick_tunnel_url,omitempty"`
	CloudInitPath    string    `json:"cloud_init_path,omitempty"`
	SnapshotsEnabled bool      `json:"snapshots_enabled,omitempty"`
	DomainBase       string    `json:"domain_base,omitempty"`
	ScenarioID       string    `json:"scenario_id,omitempty"`
	ScenarioRole     string    `json:"scenario_role,omitempty"`
}

type Share struct {
	Name      string     `json:"name"`
	URL       string     `json:"url"`
	TargetURL string     `json:"target_url"`
	Provider  string     `json:"provider"`
	PID       int        `json:"pid"`
	LogPath   string     `json:"log_path,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// ObjStore is the locally running S3-compatible object storage server. There is
// one per machine, shared by every sandbox, which is the whole point: a bucket
// has to outlive the box that wrote to it for "back up and restore" to mean
// anything.
type ObjStore struct {
	Provider  string    `json:"provider"`
	Endpoint  string    `json:"endpoint"`
	Port      int       `json:"port"`
	Region    string    `json:"region"`
	AccessKey string    `json:"access_key"`
	SecretKey string    `json:"secret_key"`
	DataDir   string    `json:"data_dir"`
	PID       int       `json:"pid"`
	LogPath   string    `json:"log_path,omitempty"`
	StartedAt time.Time `json:"started_at"`
}

// Bucket is one bucket in the local object store. The credentials live on the
// ObjStore, not here — this is only the name and when it appeared.
type Bucket struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Workspace is a private network a group of sandboxes share. Membership is the
// authoritative record of who is on the network and at which address — the
// instance registry deliberately does not mirror it, because instances are
// upserted from several code paths that would clobber a mirrored field.
type Workspace struct {
	Name      string            `json:"name"`
	CIDR      string            `json:"cidr"`
	Index     int               `json:"index"`
	CreatedAt time.Time         `json:"created_at"`
	Members   []WorkspaceMember `json:"members"`
}

// WorkspaceMember is one sandbox on a private network, with the address it was
// given. The address is stable for the life of the membership.
type WorkspaceMember struct {
	Instance  string    `json:"instance"`
	PrivateIP string    `json:"private_ip"`
	JoinedAt  time.Time `json:"joined_at"`
}

type AuthSession struct {
	Email     string     `json:"email"`
	Token     string     `json:"token,omitempty"`
	APIBase   string     `json:"api_base,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type instancesFile struct {
	Version   int        `json:"version"`
	Instances []Instance `json:"instances"`
}

type sharesFile struct {
	Version int     `json:"version"`
	Shares  []Share `json:"shares"`
}

type authFile struct {
	Version int         `json:"version"`
	Session AuthSession `json:"session"`
}

// bucketsFile holds the object store server and its buckets together, because
// the buckets are meaningless without the endpoint that serves them.
type bucketsFile struct {
	Version  int       `json:"version"`
	ObjStore *ObjStore `json:"objstore,omitempty"`
	Buckets  []Bucket  `json:"buckets"`
}

type workspacesFile struct {
	Version    int         `json:"version"`
	Workspaces []Workspace `json:"workspaces"`
}

type Store struct {
	paths config.Paths
	mu    sync.Mutex
}

func NewStore(paths config.Paths) *Store {
	return &Store{paths: paths}
}

func (s *Store) LoadInstances() ([]Instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var f instancesFile
	if err := readJSON(s.paths.RegistryPath, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	sort.Slice(f.Instances, func(i, j int) bool { return f.Instances[i].Name < f.Instances[j].Name })
	return f.Instances, nil
}

func (s *Store) SaveInstances(instances []Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sort.Slice(instances, func(i, j int) bool { return instances[i].Name < instances[j].Name })
	return writeJSON(s.paths.RegistryPath, instancesFile{
		Version:   currentVersion,
		Instances: instances,
	})
}

func (s *Store) UpsertInstance(instance Instance) error {
	instances, err := s.LoadInstances()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if instance.CreatedAt.IsZero() {
		instance.CreatedAt = now
	}
	instance.UpdatedAt = now

	replaced := false
	for i := range instances {
		if instances[i].Name == instance.Name {
			instance.CreatedAt = instances[i].CreatedAt
			instances[i] = instance
			replaced = true
			break
		}
	}

	if !replaced {
		instances = append(instances, instance)
	}

	return s.SaveInstances(instances)
}

func (s *Store) DeleteInstance(name string) error {
	instances, err := s.LoadInstances()
	if err != nil {
		return err
	}

	filtered := make([]Instance, 0, len(instances))
	for _, inst := range instances {
		if inst.Name != name {
			filtered = append(filtered, inst)
		}
	}

	return s.SaveInstances(filtered)
}

func (s *Store) GetInstance(name string) (*Instance, error) {
	instances, err := s.LoadInstances()
	if err != nil {
		return nil, err
	}

	for _, inst := range instances {
		if inst.Name == name {
			copy := inst
			return &copy, nil
		}
	}

	return nil, os.ErrNotExist
}

func (s *Store) LoadShares() ([]Share, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var f sharesFile
	if err := readJSON(s.paths.SharesPath, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	sort.Slice(f.Shares, func(i, j int) bool { return f.Shares[i].CreatedAt.After(f.Shares[j].CreatedAt) })
	return f.Shares, nil
}

func (s *Store) SaveShares(shares []Share) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.paths.SharesPath, sharesFile{
		Version: currentVersion,
		Shares:  shares,
	})
}

func (s *Store) UpsertShare(share Share) error {
	shares, err := s.LoadShares()
	if err != nil {
		return err
	}

	replaced := false
	for i := range shares {
		if shares[i].Name == share.Name {
			shares[i] = share
			replaced = true
			break
		}
	}
	if !replaced {
		shares = append(shares, share)
	}

	return s.SaveShares(shares)
}

func (s *Store) DeleteShare(name string) error {
	shares, err := s.LoadShares()
	if err != nil {
		return err
	}

	filtered := make([]Share, 0, len(shares))
	for _, share := range shares {
		if share.Name != name {
			filtered = append(filtered, share)
		}
	}

	return s.SaveShares(filtered)
}

// loadBucketsFileLocked reads buckets.json. Callers must hold s.mu. A missing
// file is an empty file, not an error — nobody has made a bucket yet.
func (s *Store) loadBucketsFileLocked() (bucketsFile, error) {
	var f bucketsFile
	if err := readJSON(s.paths.BucketsPath, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return bucketsFile{Version: bucketsVersion}, nil
		}
		return bucketsFile{}, err
	}
	return f, nil
}

// writeBucketsFileLocked persists buckets.json 0600 — it carries the object
// store's secret key, so it must not be group- or world-readable. Callers must
// hold s.mu.
func (s *Store) writeBucketsFileLocked(f bucketsFile) error {
	f.Version = bucketsVersion
	sort.Slice(f.Buckets, func(i, j int) bool { return f.Buckets[i].Name < f.Buckets[j].Name })
	return writeJSONMode(s.paths.BucketsPath, f, 0o600)
}

// LoadObjStore returns the recorded object store server, or nil if one has never
// been started on this machine.
func (s *Store) LoadObjStore() (*ObjStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadBucketsFileLocked()
	if err != nil {
		return nil, err
	}
	return f.ObjStore, nil
}

// SaveObjStore records the object store server, leaving the bucket list alone.
func (s *Store) SaveObjStore(store ObjStore) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadBucketsFileLocked()
	if err != nil {
		return err
	}
	f.ObjStore = &store
	return s.writeBucketsFileLocked(f)
}

func (s *Store) LoadBuckets() ([]Bucket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadBucketsFileLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(f.Buckets, func(i, j int) bool { return f.Buckets[i].Name < f.Buckets[j].Name })
	return f.Buckets, nil
}

func (s *Store) UpsertBucket(bucket Bucket) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadBucketsFileLocked()
	if err != nil {
		return err
	}
	if bucket.CreatedAt.IsZero() {
		bucket.CreatedAt = time.Now().UTC()
	}

	replaced := false
	for i := range f.Buckets {
		if f.Buckets[i].Name == bucket.Name {
			bucket.CreatedAt = f.Buckets[i].CreatedAt
			f.Buckets[i] = bucket
			replaced = true
			break
		}
	}
	if !replaced {
		f.Buckets = append(f.Buckets, bucket)
	}

	return s.writeBucketsFileLocked(f)
}

func (s *Store) DeleteBucket(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := s.loadBucketsFileLocked()
	if err != nil {
		return err
	}

	filtered := make([]Bucket, 0, len(f.Buckets))
	for _, bucket := range f.Buckets {
		if bucket.Name != name {
			filtered = append(filtered, bucket)
		}
	}
	f.Buckets = filtered
	return s.writeBucketsFileLocked(f)
}

func (s *Store) GetBucket(name string) (*Bucket, error) {
	buckets, err := s.LoadBuckets()
	if err != nil {
		return nil, err
	}

	for _, bucket := range buckets {
		if bucket.Name == name {
			copy := bucket
			return &copy, nil
		}
	}

	return nil, os.ErrNotExist
}

func (s *Store) LoadWorkspaces() ([]Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var f workspacesFile
	if err := readJSON(s.paths.WorkspacesPath, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	sort.Slice(f.Workspaces, func(i, j int) bool { return f.Workspaces[i].Name < f.Workspaces[j].Name })
	return f.Workspaces, nil
}

func (s *Store) SaveWorkspaces(workspaces []Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sort.Slice(workspaces, func(i, j int) bool { return workspaces[i].Name < workspaces[j].Name })
	return writeJSON(s.paths.WorkspacesPath, workspacesFile{
		Version:    workspacesVersion,
		Workspaces: workspaces,
	})
}

func (s *Store) UpsertWorkspace(workspace Workspace) error {
	workspaces, err := s.LoadWorkspaces()
	if err != nil {
		return err
	}

	if workspace.CreatedAt.IsZero() {
		workspace.CreatedAt = time.Now().UTC()
	}
	sort.Slice(workspace.Members, func(i, j int) bool {
		return workspace.Members[i].Instance < workspace.Members[j].Instance
	})

	replaced := false
	for i := range workspaces {
		if workspaces[i].Name == workspace.Name {
			workspace.CreatedAt = workspaces[i].CreatedAt
			workspaces[i] = workspace
			replaced = true
			break
		}
	}
	if !replaced {
		workspaces = append(workspaces, workspace)
	}

	return s.SaveWorkspaces(workspaces)
}

func (s *Store) DeleteWorkspace(name string) error {
	workspaces, err := s.LoadWorkspaces()
	if err != nil {
		return err
	}

	filtered := make([]Workspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		if workspace.Name != name {
			filtered = append(filtered, workspace)
		}
	}

	return s.SaveWorkspaces(filtered)
}

func (s *Store) GetWorkspace(name string) (*Workspace, error) {
	workspaces, err := s.LoadWorkspaces()
	if err != nil {
		return nil, err
	}

	for _, workspace := range workspaces {
		if workspace.Name == name {
			copy := workspace
			return &copy, nil
		}
	}

	return nil, os.ErrNotExist
}

// WorkspaceForInstance finds the workspace a sandbox belongs to, if any. A
// sandbox is on at most one private network — joining a second would give it a
// route between two networks that are supposed to be isolated.
func (s *Store) WorkspaceForInstance(instance string) (*Workspace, error) {
	workspaces, err := s.LoadWorkspaces()
	if err != nil {
		return nil, err
	}

	for _, workspace := range workspaces {
		for _, member := range workspace.Members {
			if member.Instance == instance {
				copy := workspace
				return &copy, nil
			}
		}
	}

	return nil, os.ErrNotExist
}

func (s *Store) SaveAuth(session AuthSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return writeJSON(s.paths.AuthPath, authFile{
		Version: currentVersion,
		Session: session,
	})
}

func (s *Store) LoadAuth() (*AuthSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var f authFile
	if err := readJSON(s.paths.AuthPath, &f); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	return &f.Session, nil
}

func (s *Store) DeleteAuth() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.paths.AuthPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func readJSON(path string, out any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, out)
}

func writeJSON(path string, value any) error {
	return writeJSONMode(path, value, 0o644)
}

func writeJSONMode(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.WriteFile(path, data, mode); err != nil {
		return err
	}
	// WriteFile only applies mode when it creates the file, so an existing file
	// keeps whatever permissions it had. Secrets need the mode either way.
	return os.Chmod(path, mode)
}
