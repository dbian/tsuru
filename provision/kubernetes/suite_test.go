// Copyright 2012 tsuru authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package kubernetes

import (
	"math/rand"
	"testing"

	"github.com/tsuru/config"
	"github.com/tsuru/tsuru/app"
	"github.com/tsuru/tsuru/auth"
	"github.com/tsuru/tsuru/auth/native"
	"github.com/tsuru/tsuru/db"
	"github.com/tsuru/tsuru/db/dbtest"
	"github.com/tsuru/tsuru/provision/cluster"
	kTesting "github.com/tsuru/tsuru/provision/kubernetes/testing"
	"github.com/tsuru/tsuru/provision/pool"
	"github.com/tsuru/tsuru/quota"
	"github.com/tsuru/tsuru/router/routertest"
	"github.com/tsuru/tsuru/servicemanager"
	_ "github.com/tsuru/tsuru/storage/mongodb"
	appTypes "github.com/tsuru/tsuru/types/app"
	authTypes "github.com/tsuru/tsuru/types/auth"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/check.v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
)

type S struct {
	p             *kubernetesProvisioner
	conn          *db.Storage
	user          *auth.User
	team          *authTypes.Team
	token         auth.Token
	client        *kTesting.ClientWrapper
	clusterClient *ClusterClient
	t             *testing.T
	mock          *kTesting.KubeMock
	mockService   struct {
		Team *authTypes.MockTeamService
		Plan *appTypes.MockPlanService
	}
}

var suiteInstance = &S{}
var _ = check.Suite(suiteInstance)
var defaultClientForConfig = ClientForConfig

func Test(t *testing.T) {
	suiteInstance.t = t
	check.TestingT(t)
}

func (s *S) SetUpSuite(c *check.C) {
	config.Set("log:disable-syslog", true)
	config.Set("auth:hash-cost", bcrypt.MinCost)
	config.Set("database:driver", "mongodb")
	config.Set("database:url", "127.0.0.1:27017?maxPoolSize=100")
	config.Set("database:name", "provision_kubernetes_tests_s")
	config.Set("routers:fake:type", "fake")
	config.Set("routers:fake:default", true)
	var err error
	s.conn, err = db.Conn()
	c.Assert(err, check.IsNil)
}

func (s *S) TearDownSuite(c *check.C) {
	s.conn.Close()
}

func (s *S) SetUpTest(c *check.C) {
	err := dbtest.ClearAllCollections(s.conn.Apps().Database)
	c.Assert(err, check.IsNil)
	clus := &cluster.Cluster{
		Name:        "c1",
		Addresses:   []string{"https://clusteraddr"},
		Default:     true,
		Provisioner: provisionerName,
		CustomData:  map[string]string{},
	}
	err = clus.Save()
	c.Assert(err, check.IsNil)
	s.clusterClient, err = NewClusterClient(clus)
	c.Assert(err, check.IsNil)
	s.client = &kTesting.ClientWrapper{
		Clientset:        fake.NewSimpleClientset(),
		ClusterInterface: s.clusterClient,
	}
	s.clusterClient.Interface = s.client
	ClientForConfig = func(conf *rest.Config) (kubernetes.Interface, error) {
		return s.client, nil
	}
	routertest.FakeRouter.Reset()
	rand.Seed(0)
	err = pool.AddPool(pool.AddPoolOptions{
		Name:        "test-default",
		Default:     true,
		Provisioner: "kubernetes",
	})
	c.Assert(err, check.IsNil)
	s.p = &kubernetesProvisioner{}
	s.mock = kTesting.NewKubeMock(s.client, s.p)
	s.user = &auth.User{Email: "whiskeyjack@genabackis.com", Password: "123456", Quota: quota.Unlimited}
	nativeScheme := auth.ManagedScheme(native.NativeScheme{})
	app.AuthScheme = nativeScheme
	_, err = nativeScheme.Create(s.user)
	c.Assert(err, check.IsNil)
	s.team = &authTypes.Team{Name: "admin"}
	s.token, err = nativeScheme.Login(map[string]string{"email": s.user.Email, "password": "123456"})
	c.Assert(err, check.IsNil)
	s.mockService.Team = &authTypes.MockTeamService{
		OnList: func() ([]authTypes.Team, error) {
			return []authTypes.Team{*s.team}, nil
		},
		OnFindByName: func(_ string) (*authTypes.Team, error) {
			return s.team, nil
		},
		OnFindByNames: func(_ []string) ([]authTypes.Team, error) {
			return []authTypes.Team{{Name: s.team.Name}}, nil
		},
	}
	plan := appTypes.Plan{
		Name:     "default",
		Default:  true,
		CpuShare: 100,
	}
	s.mockService.Plan = &appTypes.MockPlanService{
		OnList: func() ([]appTypes.Plan, error) {
			return []appTypes.Plan{plan}, nil
		},
		OnDefaultPlan: func() (*appTypes.Plan, error) {
			return &plan, nil
		},
	}
	servicemanager.Team = s.mockService.Team
	servicemanager.Plan = s.mockService.Plan
}
