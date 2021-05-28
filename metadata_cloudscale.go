package main

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/gofrs/uuid"
)

const (
	cloudscaleMetadataURL string = "http://169.254.169.254/openstack/latest/meta_data.json"
)

type cloudscaleMetadata struct {
	Name string `json:"name"`
	Meta struct {
		CloudscaleUUID *uuid.UUID `json:"cloudscale_uuid"`
	} `json:"meta"`
}

func findCloudscaleServerMetadata() (*cloudscaleMetadata, error) {
	var md *cloudscaleMetadata

	req, err := http.NewRequest("GET", cloudscaleMetadataURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Add("User-Agent", newVersionInfo().HTTPUserAgent())

	client := http.Client{
		Timeout: 5 * time.Second,
	}

	fn := func() error {
		resp, err := client.Do(req)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		// Limit accepted response size
		lr := io.LimitReader(resp.Body, 1024*1024)

		body, err := ioutil.ReadAll(lr)
		if err != nil {
			return err
		}

		md = &cloudscaleMetadata{}

		if err = json.Unmarshal(body, md); err != nil {
			return err
		}

		return err
	}

	if err := metadataRetry(fn); err != nil {
		return nil, err
	}

	return md, nil
}
