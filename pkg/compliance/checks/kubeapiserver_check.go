// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License Version 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2016-2020 Datadog, Inc.

package checks

import (
	"fmt"

	"github.com/DataDog/datadog-agent/pkg/compliance"
	"github.com/DataDog/datadog-agent/pkg/util/json"
	"github.com/DataDog/datadog-agent/pkg/util/log"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeDynamic "k8s.io/client-go/dynamic"
)

type kubeApiserverCheck struct {
	baseCheck
	kubeClient   kubeDynamic.Interface
	kubeResource compliance.KubernetesResource
}

const (
	kubeResourceNameKey      string = "kube_resource_name"
	kubeResourceGroupKey     string = "kube_resource_group"
	kubeResourceVersionKey   string = "kube_resource_version"
	kubeResourceNamespaceKey string = "kube_resource_namespace"
	kubeResourceKindKey      string = "kube_resource_kind"
)

func newKubeapiserverCheck(baseCheck baseCheck, kubeResource *compliance.KubernetesResource, kubeClient kubeDynamic.Interface) (*kubeApiserverCheck, error) {
	check := &kubeApiserverCheck{
		baseCheck:    baseCheck,
		kubeClient:   kubeClient,
		kubeResource: *kubeResource,
	}

	if len(check.kubeResource.Kind) == 0 {
		return nil, fmt.Errorf("Cannot create Kubeapiserver check, resource kind is empty, rule: %s", baseCheck.ruleID)
	}

	if len(check.kubeResource.APIRequest.Verb) == 0 {
		return nil, fmt.Errorf("Cannot create Kubeapiserver check, action verb is empty, rule: %s", baseCheck.ruleID)
	}

	if len(check.kubeResource.Version) == 0 {
		check.kubeResource.Version = "v1"
	}

	return check, nil
}

func (c *kubeApiserverCheck) Run() error {
	log.Debugf("%s: kubeapiserver check: %v", c.ruleID, c.kubeResource)

	resourceSchema := schema.GroupVersionResource{
		Group:    c.kubeResource.Group,
		Resource: c.kubeResource.Kind,
		Version:  c.kubeResource.Version,
	}
	resourceDef := c.kubeClient.Resource(resourceSchema)

	var resourceAPI kubeDynamic.ResourceInterface
	if len(c.kubeResource.Namespace) > 0 {
		resourceAPI = resourceDef.Namespace(c.kubeResource.Namespace)
	} else {
		resourceAPI = resourceDef
	}

	var resources []unstructured.Unstructured
	switch c.kubeResource.APIRequest.Verb {
	case "get":
		if len(c.kubeResource.APIRequest.ResourceName) == 0 {
			return fmt.Errorf("%s: unable to use 'get' apirequest without resource name", c.ruleID)
		}
		resource, err := resourceAPI.Get(c.kubeResource.APIRequest.ResourceName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("Unable to get Kube resource:'%v', ns:'%s' name:'%s', err: %v", resourceSchema, c.kubeResource.Namespace, c.kubeResource.APIRequest.ResourceName, err)
		}
		resources = []unstructured.Unstructured{*resource}
	case "list":
		list, err := resourceAPI.List(metav1.ListOptions{})
		if err != nil {
			return fmt.Errorf("Unable to list Kube resources:'%v', ns:'%s' name:'%s', err: %v", resourceSchema, c.kubeResource.Namespace, c.kubeResource.APIRequest.ResourceName, err)
		}
		resources = list.Items
	}

	log.Debugf("%s: Got %d resources", c.ruleID, len(resources))
	for _, resource := range resources {
		if err := c.reportResource(resource); err != nil {
			return err
		}
	}

	return nil
}

func (c *kubeApiserverCheck) reportResource(p unstructured.Unstructured) error {
	kv := compliance.KVMap{}

	for _, field := range c.kubeResource.Report {
		switch field.Kind {
		case compliance.PropertyKindJSONQuery:
			reportValue, valueFound, err := json.RunSingleOutput(field.Property, p.Object)
			if err != nil {
				return fmt.Errorf("Unable to report field: '%s' for kubernetes object '%s / %s / %s' - json query error: %v", field.Property, p.GroupVersionKind().String(), p.GetNamespace(), p.GetName(), err)
			}

			if !valueFound {
				continue
			}

			reportName := field.Property
			if len(field.As) > 0 {
				reportName = field.As
			}
			if len(field.Value) > 0 {
				reportValue = field.Value
			}

			kv[reportName] = reportValue
		default:
			return fmt.Errorf("Unsupported kind value: '%s' for KubeResource", field.Kind)
		}
	}

	if len(kv) > 0 {
		kv[kubeResourceKindKey] = p.GetObjectKind().GroupVersionKind().Kind
		kv[kubeResourceGroupKey] = p.GetObjectKind().GroupVersionKind().Group
		kv[kubeResourceVersionKey] = p.GetObjectKind().GroupVersionKind().Version
		kv[kubeResourceNamespaceKey] = p.GetNamespace()
		kv[kubeResourceNameKey] = p.GetName()
	}

	c.report(nil, kv)
	return nil
}
