package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"github.com/ciscoecosystem/mso-go-client/container"
	"github.com/ciscoecosystem/mso-go-client/models"
)

func (c *Client) GetViaURL(endpoint string) (*container.Container, error) {

	req, err := c.MakeRestRequest("GET", endpoint, nil, true)

	if err != nil {
		return nil, err
	}

	obj, _, err := c.Do(req)
	if err != nil {
		return nil, err
	}

	if obj == nil {
		return nil, errors.New("Empty response body")
	}
	return obj, CheckForErrors(obj, "GET")

}

func (c *Client) GetViaURLWithCache(url string) (*container.Container, error) {
	if !c.cacheEnabled {
		return c.GetViaURL(url)
	}

	cacheKey := url

	passthroughFunc := func(item interface{}) (interface{}, error) {
		return item, nil
	}

	if cached, found, err := c.Cache.Get(cacheKey, passthroughFunc); found {
		if err != nil {
			log.Printf("[WARN] Cache error for %s, fetching fresh: %v", url, err)
			return c.GetViaURL(url)
		}

		resourceType := c.detectURLResourceType(url)
		c.Cache.LogEvent(resourceType+"_CACHE_HIT", url)

		jsonBytes := cached.([]byte)

		cont, err := container.ParseJSON(jsonBytes)
		if err != nil {
			log.Printf("[WARN] Failed to parse cached JSON for %s, fetching fresh: %v", url, err)
			return c.GetViaURL(url)
		}
		return cont, nil
	}

	resourceType := c.detectURLResourceType(url)
	c.Cache.LogEvent(resourceType+"_CACHE_MISS", url)

	// Cache miss - fetch from API
	cont, err := c.GetViaURL(url)
	if err != nil {
		return nil, err
	}

	jsonBytes, err := json.Marshal(cont.Data())
	if err != nil {
		log.Printf("[WARN] Failed to marshal %s for caching, proceeding without cache: %v", url, err)
		return cont, nil
	}

	c.Cache.Set(cacheKey, jsonBytes)
	c.Cache.LogEvent(resourceType+"_CACHED", url, true)

	return cont, nil
}

// detectURLResourceType detects resource type from URL for better logging and monitoring
func (c *Client) detectURLResourceType(url string) string {
	// Detect common MSO resource types from URL patterns
	if strings.Contains(url, "/schemas/") {
		return "SCHEMA"
	}
	if strings.Contains(url, "/templates/") {
		return "TEMPLATE"
	}
	if strings.Contains(url, "/sites/") {
		return "SITE"
	}
	if strings.Contains(url, "/users/") {
		return "USER"
	}
	if strings.Contains(url, "/tenants/") {
		return "TENANT"
	}
	if strings.Contains(url, "/labels/") {
		return "LABEL"
	}
	if strings.Contains(url, "/remote-locations/") {
		return "REMOTE_LOCATION"
	}
	// Generic fallback for unknown resource types
	return "RESOURCE"
}

// InvalidateURLCache removes a URL from cache
func (c *Client) InvalidateURLCache(url string) {
	// Skip cache operations if caching is disabled
	if !c.cacheEnabled {
		return
	}

	cacheKey := url // URL-based cache key
	c.Cache.Delete(cacheKey)

	resourceType := c.detectURLResourceType(url)
	c.Cache.LogEvent(resourceType+"_CACHE_INVALIDATED", url)
}

func (c *Client) GetPlatform() string {
	return c.platform
}

func (c *Client) Put(endpoint string, obj models.Model) (*container.Container, error) {
	jsonPayload, err := c.PrepareModel(obj)

	if err != nil {
		return nil, err
	}
	req, err := c.MakeRestRequest("PUT", endpoint, jsonPayload, true)
	if err != nil {
		return nil, err
	}

	c.Mutex.Lock()
	cont, _, err := c.Do(req)
	c.Mutex.Unlock()
	if err != nil {
		return nil, err
	}

	return cont, CheckForErrors(cont, "PUT")
}

func (c *Client) Save(endpoint string, obj models.Model) (*container.Container, error) {

	jsonPayload, err := c.PrepareModel(obj)

	if err != nil {
		return nil, err
	}
	req, err := c.MakeRestRequest("POST", endpoint, jsonPayload, true)
	if err != nil {
		return nil, err
	}

	cont, _, err := c.Do(req)
	if err != nil {
		return nil, err
	}

	return cont, CheckForErrors(cont, "POST")
}

// CheckForErrors parses the response and checks of there is an error attribute in the response
func CheckForErrors(cont *container.Container, method string) error {

	if cont.Exists("code") && cont.Exists("message") {
		return errors.New(fmt.Sprintf("%s%s", cont.S("message"), cont.S("info")))
	} else if cont.Exists("error") {
		return errors.New(fmt.Sprintf("%s %s", models.StripQuotes(cont.S("error").String()), models.StripQuotes(cont.S("error_code").String())))
	}
	return nil
}

func (c *Client) DeletebyId(url string) error {

	req, err := c.MakeRestRequest("DELETE", url, nil, true)
	if err != nil {
		return err
	}

	_, resp, err1 := c.Do(req)
	if err1 != nil {
		return err1
	}
	if resp != nil {
		if resp.StatusCode == 204 || resp.StatusCode == 200 {
			return nil
		} else {
			return fmt.Errorf("Unable to delete the object")
		}
	}

	return nil
}

func (c *Client) PatchbyID(endpoint string, objList ...models.Model) (*container.Container, error) {

	contJs := container.New()
	contJs.Array()
	for _, obj := range objList {
		jsonPayload, err := c.PrepareModel(obj)
		if err != nil {
			return nil, err
		}
		contJs.ArrayAppend(jsonPayload.Data())

	}
	log.Printf("[DEBUG] Patch Request Container: %v\n", contJs)
	// URL encoding
	baseUrl, _ := url.Parse(endpoint)
	qs := url.Values{}
	qs.Add("validate", "false")
	baseUrl.RawQuery = qs.Encode()

	req, err := c.MakeRestRequest("PATCH", baseUrl.String(), contJs, true)
	if err != nil {
		return nil, err
	}

	c.Mutex.Lock()
	cont, _, err := c.Do(req)
	c.Mutex.Unlock()
	if err != nil {
		return nil, err
	}

	return cont, CheckForErrors(cont, "PATCH")
}

func (c *Client) PrepareModel(obj models.Model) (*container.Container, error) {
	con, err := obj.ToMap()
	if err != nil {
		return nil, err
	}

	payload := &container.Container{}
	if err != nil {
		return nil, err
	}

	for key, value := range con {
		payload.Set(value, key)
	}
	return payload, nil
}
