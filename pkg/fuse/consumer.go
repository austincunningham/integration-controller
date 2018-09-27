package fuse

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"

	"k8s.io/apimachinery/pkg/api/errors"

	enmasse "github.com/integr8ly/integration-controller/pkg/apis/enmasse/v1"
	"github.com/integr8ly/integration-controller/pkg/apis/integration/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	pkgerrs "github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	v1alpha12 "github.com/syndesisio/syndesis/install/operator/pkg/apis/syndesis/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
)

type Consumer struct {
	watchNS    string
	fuseCruder FuseCrudler
}

func NewConsumer(watchNS string, fuseCruder FuseCrudler) *Consumer {
	return &Consumer{watchNS: watchNS, fuseCruder: fuseCruder}
}

func (c *Consumer) GVKs() []schema.GroupVersionKind {
	return []schema.GroupVersionKind{{
		Kind:    enmasse.AddressSpaceKind,
		Group:   enmasse.GroupName,
		Version: enmasse.Version,
	}}
}

func (c *Consumer) Exists() bool {
	logrus.Debug("fuse consume: checking if a fuse exists")
	syndesisList := v1alpha12.NewSyndesisList()
	if err := c.fuseCruder.List(c.watchNS, syndesisList); err != nil {
		logrus.Error("fuse consumer: failed to check if fuse exists ", err)
		return false
	}

	return len(syndesisList.Items) > 0
}

func (c *Consumer) Validate(object runtime.Object) error {
	// validate that we have the annotations
	switch o := object.(type) {
	case *enmasse.AddressSpace:
		if o.Annotations == nil || o.Annotations["enmasse.io/realm-name"] == "" || o.Annotations["enmasse.io/created-by"] == "" {
			return pkgerrs.New("fuse consumer: enmasse address space invalid. missing annotations. Needed: (enmasse.io/realm-name,enmasse.io/created-by)")
		}
	}
	return nil
}

func (c *Consumer) CreateAvailableIntegration(o runtime.Object, namespace string, enabled bool) error {
	logrus.Info("create available integration for fuses")
	as := o.(*enmasse.AddressSpace)
	syndesisList := v1alpha12.NewSyndesisList()
	if err := sdk.List(c.watchNS, syndesisList); err != nil {
		logrus.Error("fuse consumer: failed to check if fuse exists ", err)
		return nil
	}
	var errs error
	for _, s := range syndesisList.Items {
		if as.Annotations == nil || s.Annotations == nil {
			continue
		}
		// only create if the same use owns both
		if strings.TrimSpace(as.Annotations["enmasse.io/created-by"]) != strings.TrimSpace(s.Annotations["syndesis.io/created-by"]) {
			logrus.Debug("found an enmasse address space but it does not match the user for fuse")
			continue
		}
		for _, endPointStatus := range as.Status.EndPointStatuses {
			if endPointStatus.Name == "messaging" {
				ingrtn := v1alpha1.NewIntegration()
				ingrtn.ObjectMeta.Name = c.integrationName(o)
				ingrtn.ObjectMeta.Namespace = namespace
				ingrtn.Spec.Client = s.Name
				ingrtn.Status.IntegrationMetaData = map[string]string{}
				ingrtn.Status.IntegrationMetaData[msgHostKey] = endPointStatus.ServiceHost + ":" + fmt.Sprintf("%d", endPointStatus.Port)
				ingrtn.Status.IntegrationMetaData[realmKey] = as.Annotations["enmasse.io/realm-name"]
				ingrtn.Spec.IntegrationType = "amqp"
				ingrtn.Spec.ServiceProvider = string(v1alpha1.FuseIntegrationTarget)
				ingrtn.Spec.Enabled = enabled

				for _, servicePort := range endPointStatus.ServicePorts {
					if servicePort.Name == "amqp" {
						ingrtn.Status.IntegrationMetaData[msgHostKey] = endPointStatus.ServiceHost + ":" + fmt.Sprintf("%d", servicePort.Port)
					}

				}

				if err := sdk.Create(ingrtn); err != nil && !errors.IsAlreadyExists(err) {
					if errs == nil {
						errs = err
						continue
					}
					errs = pkgerrs.Wrap(errs, err.Error())
				}
			}
		}
	}

	return errs
}

func (c *Consumer) RemoveAvailableIntegration(o runtime.Object, namespace string) error {
	logrus.Info("delete available integration called for fuse")
	// get an integration with that name
	name := c.integrationName(o)
	ingrtn := v1alpha1.NewIntegration()
	ingrtn.ObjectMeta.Name = name
	ingrtn.ObjectMeta.Namespace = namespace
	if err := sdk.Get(ingrtn); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return sdk.Delete(ingrtn)
}

func (c *Consumer) integrationName(o runtime.Object) string {
	as := o.(*enmasse.AddressSpace)
	return "enmasse-" + as.Name + "-to-fuse"
}

//go:generate moq -out fuse_crudler_test.go . FuseCrudler
type FuseCrudler interface {
	List(namespace string, o sdk.Object, option ...sdk.ListOption) error
}