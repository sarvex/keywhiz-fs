// Copyright 2015 Square Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package keywhizfs_test

import (
	"testing"
	"time"

	"github.com/square/keywhizfs"
	"github.com/square/keywhizfs/log"
	"github.com/stretchr/testify/assert"
)

var logConfig = log.Config{Debug: false, Mountpoint: "/tmp/mnt"}

// FailingBackend always returns ok==false
type FailingBackend struct {
}

func (b FailingBackend) Secret(name string) (*keywhizfs.Secret, bool) {
	return nil, false
}

func (b FailingBackend) SecretList() ([]keywhizfs.Secret, bool) {
	return nil, false
}

// ChannelBackend reads values from channels to return or blocks.
type ChannelBackend struct {
	secretc     chan *keywhizfs.Secret
	secretListc chan []keywhizfs.Secret
}

func (b ChannelBackend) Secret(name string) (*keywhizfs.Secret, bool) {
	secret := <-b.secretc
	return secret, true
}

func (b ChannelBackend) SecretList() ([]keywhizfs.Secret, bool) {
	secretList := <-b.secretListc
	return secretList, true
}

var timeouts = keywhizfs.Timeouts{0, 10 * time.Millisecond, 20 * time.Millisecond}

func TestCacheSecretUsesValuesFromClient(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := keywhizfs.ParseSecret(fixture("secret.json"))

	secretc := make(chan *keywhizfs.Secret, 1)
	backend := ChannelBackend{secretc: secretc}
	secretc <- secretFixture

	cache := keywhizfs.NewCache(backend, timeouts, logConfig)
	secret, ok := cache.Secret("password-file")
	assert.True(ok)
	assert.Equal(secretFixture, secret)
}

func TestCachePassesThroughSecretNotFound(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := keywhizfs.ParseSecret(fixture("secret.json"))

	cache := keywhizfs.NewCache(FailingBackend{}, timeouts, logConfig)
	secret, ok := cache.Secret(secretFixture.Name)
	assert.False(ok)
	assert.Nil(secret)

	cache.Add(*secretFixture)
	secret, ok = cache.Secret(secretFixture.Name)
	assert.True(ok)
	assert.Equal(secretFixture, secret)
}

func TestCacheSecretWhenClientTimesOut(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := keywhizfs.ParseSecret(fixture("secret.json"))
	backend := ChannelBackend{} // channels are nil and will block
	cache := keywhizfs.NewCache(backend, timeouts, logConfig)

	// empty cache
	secret, ok := cache.Secret(secretFixture.Name)
	assert.False(ok)
	assert.Nil(secret)

	// cache with entry
	cache.Add(*secretFixture)
	secret, ok = cache.Secret(secretFixture.Name)
	assert.True(ok)
	assert.Equal(secretFixture, secret)
}

func TestCacheSecretUsesClientOverCache(t *testing.T) {
	assert := assert.New(t)

	fixture1, _ := keywhizfs.ParseSecret(fixture("secret.json"))
	fixture2, _ := keywhizfs.ParseSecret(fixture("secretNormalOwner.json"))
	fixture2.Name = fixture1.Name

	secretc := make(chan *keywhizfs.Secret, 1)
	backend := ChannelBackend{secretc: secretc}
	secretc <- fixture1

	cache := keywhizfs.NewCache(backend, timeouts, logConfig)
	cache.Add(*fixture2)

	// Although fixture2 is in the cache, the client returns fixture1.
	secret, ok := cache.Secret(fixture2.Name)
	assert.True(ok)
	assert.Equal(fixture1, secret)

	assert.Equal(1, cache.Len())
}

func TestCacheSecretAvoidsBackendWhenResultFresh(t *testing.T) {
	assert := assert.New(t)

	fixture1, _ := keywhizfs.ParseSecret(fixture("secret.json"))
	fixture2, _ := keywhizfs.ParseSecret(fixture("secretNormalOwner.json"))
	fixture2.Name = fixture1.Name

	// Backend has fixture1, cache has fixture2
	secretc := make(chan *keywhizfs.Secret, 1)
	backend := ChannelBackend{secretc: secretc}
	secretc <- fixture1

	// 1 Hour fresh threshold is sure to be fresh
	timeouts := keywhizfs.Timeouts{1 * time.Hour, 10 * time.Millisecond, 20 * time.Millisecond}
	cache := keywhizfs.NewCache(backend, timeouts, logConfig)
	cache.Add(*fixture2)

	secret, ok := cache.Secret(fixture2.Name)
	assert.True(ok)
	assert.Equal(fixture2, secret)
	secret, ok = cache.Secret(fixture2.Name)
	assert.True(ok)
	assert.Equal(fixture2, secret)

	// 1 Nanosecond fresh threshold is sure to make a server request
	timeouts = keywhizfs.Timeouts{1 * time.Nanosecond, 10 * time.Millisecond, 20 * time.Millisecond}
	cache = keywhizfs.NewCache(backend, timeouts, logConfig)
	cache.Add(*fixture2)
	time.Sleep(2 * time.Nanosecond)

	secret, ok = cache.Secret(fixture2.Name)
	assert.True(ok)
	assert.Equal(fixture1, secret) // fixture1 comes form the backend
}

func TestCacheSecretListUsesValuesFromCacheIfClientFails(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := keywhizfs.ParseSecret(fixture("secret.json"))

	cache := keywhizfs.NewCache(FailingBackend{}, timeouts, logConfig)
	cache.Add(*secretFixture)
	list := cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)
}

func TestCacheSecretListWhenClientTimesOut(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := keywhizfs.ParseSecret(fixture("secret.json"))
	backend := ChannelBackend{} // channels are nil and will block
	cache := keywhizfs.NewCache(backend, timeouts, logConfig)

	// cache empty
	list := cache.SecretList()
	assert.Empty(list)

	// cache with entry
	cache.Add(*secretFixture)
	list = cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)
}

func TestCacheSecretListUsesValuesFromClient(t *testing.T) {
	assert := assert.New(t)

	secretFixture, _ := keywhizfs.ParseSecret(fixture("secret.json"))

	secretListc := make(chan []keywhizfs.Secret, 1)
	backend := ChannelBackend{secretListc: secretListc}
	secretListc <- []keywhizfs.Secret{*secretFixture}

	cache := keywhizfs.NewCache(backend, timeouts, logConfig)
	list := cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *secretFixture)

	assert.Equal(1, cache.Len())
}

func TestCacheSecretListUsesClientOverCache(t *testing.T) {
	assert := assert.New(t)

	fixture1, _ := keywhizfs.ParseSecret(fixture("secret.json"))
	fixture2, _ := keywhizfs.ParseSecret(fixture("secretNormalOwner.json"))

	secretListc := make(chan []keywhizfs.Secret, 1)
	backend := ChannelBackend{secretListc: secretListc}
	secretListc <- []keywhizfs.Secret{*fixture1}

	cache := keywhizfs.NewCache(backend, timeouts, logConfig)
	cache.Add(*fixture2)

	// Although fixture2 is in the cache, the client says only fixture1 available.
	list := cache.SecretList()
	assert.Len(list, 1)
	assert.Contains(list, *fixture1)

	assert.Equal(1, cache.Len())
}

func TestCacheClears(t *testing.T) {
	assert := assert.New(t)

	cache := keywhizfs.NewCache(nil, timeouts, logConfig)

	secretFixture, _ := keywhizfs.ParseSecret(fixture("secret.json"))
	cache.Add(*secretFixture)
	assert.NotEqual(0, cache.Len())

	cache.Clear()
	assert.Equal(0, cache.Len())
}
