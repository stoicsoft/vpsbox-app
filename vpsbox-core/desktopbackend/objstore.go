package desktopbackend

import (
	"context"
	"fmt"
	"strings"
)

// ObjectStore is the local S3 endpoint, shaped for the frontend. The secret key
// is included because the Buckets tab shows it — it is a local-only credential
// the user is expected to paste into their own app config.
type ObjectStore struct {
	Running      bool     `json:"running"`
	Installed    bool     `json:"installed"`
	Endpoint     string   `json:"endpoint"`     // as reached from a sandbox
	HostEndpoint string   `json:"hostEndpoint"` // as reached from this machine
	Region       string   `json:"region"`
	AccessKey    string   `json:"accessKey"`
	SecretKey    string   `json:"secretKey"`
	Buckets      []Bucket `json:"buckets"`
}

// Bucket is one bucket in the local object store.
type Bucket struct {
	Name      string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

// GetObjectStore reports the object store without starting it, so the Buckets tab
// can render before the user has ever asked for storage.
func (a *App) GetObjectStore() (ObjectStore, error) {
	if a.manager == nil {
		return ObjectStore{}, fmt.Errorf("desktop backend is not ready")
	}

	access, err := a.manager.BucketStatus(context.Background())
	if err != nil {
		return ObjectStore{}, err
	}

	out := ObjectStore{
		Running:      access.Running,
		Installed:    a.manager.BucketsInstalled(),
		Endpoint:     access.Endpoint,
		HostEndpoint: access.HostEndpoint,
		Region:       access.Region,
		AccessKey:    access.AccessKey,
		SecretKey:    access.SecretKey,
		Buckets:      make([]Bucket, 0, len(access.Buckets)),
	}
	for _, bucket := range access.Buckets {
		out.Buckets = append(out.Buckets, Bucket{
			Name:      bucket.Name,
			CreatedAt: bucket.CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	return out, nil
}

// StartCreateBucket creates a bucket. The first call on a machine may install the
// object storage server, so this is a job rather than a blocking call.
func (a *App) StartCreateBucket(name string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("bucket name is required")
	}

	job := a.newJob("bucket-create", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Creating bucket "+name)
		_, err := a.manager.CreateBucket(context.Background(), name,
			func(message string) { a.updateJobMessage(job.ID, message) },
		)
		if err != nil {
			return err
		}
		a.updateJobMessage(job.ID, "Bucket "+name+" is ready")
		return nil
	})
	return job.ID, nil
}

// StartDeleteBucket deletes a bucket. force also discards its contents, which is
// the one destructive action here that no sandbox snapshot can undo.
func (a *App) StartDeleteBucket(name string, force bool) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("bucket name is required")
	}

	job := a.newJob("bucket-delete", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Deleting bucket "+name)
		if err := a.manager.DestroyBucket(context.Background(), name, force); err != nil {
			return err
		}
		a.updateJobMessage(job.ID, "Bucket "+name+" deleted")
		return nil
	})
	return job.ID, nil
}

// StartAttachBuckets wires a sandbox up to the object store. It SSHes in and may
// install the AWS CLI, so it streams progress like a deploy.
func (a *App) StartAttachBuckets(name string) (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("sandbox name is required")
	}

	job := a.newJob("bucket-attach", name)
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Connecting "+name+" to the object store")
		_, err := a.manager.AttachBuckets(context.Background(), name,
			func(message string) { a.updateJobMessage(job.ID, message) },
			func(line string) { a.appendJobLog(job.ID, line) },
		)
		if err != nil {
			return err
		}
		a.updateJobMessage(job.ID, name+" can now reach the object store")
		return nil
	})
	return job.ID, nil
}

// StartStopObjectStore shuts the server down, keeping bucket data on disk.
func (a *App) StartStopObjectStore() (string, error) {
	if a.manager == nil {
		return "", fmt.Errorf("desktop backend is not ready")
	}

	job := a.newJob("bucket-stop", "")
	go a.runJob(job.ID, func(job *Job) error {
		a.updateJobMessage(job.ID, "Stopping the object store")
		if err := a.manager.StopBuckets(); err != nil {
			return err
		}
		a.updateJobMessage(job.ID, "Object store stopped — bucket data kept")
		return nil
	})
	return job.ID, nil
}
