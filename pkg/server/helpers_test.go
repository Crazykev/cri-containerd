/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package server

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"

	"github.com/kubernetes-incubator/cri-containerd/pkg/metadata"
	ostesting "github.com/kubernetes-incubator/cri-containerd/pkg/os/testing"
)

func TestPrepareStreamingPipes(t *testing.T) {
	for desc, test := range map[string]struct {
		stdin  string
		stdout string
		stderr string
	}{
		"empty stdin": {
			stdout: "/test/stdout",
			stderr: "/test/stderr",
		},
		"empty stdout/stderr": {
			stdin: "/test/stdin",
		},
		"non-empty stdio": {
			stdin:  "/test/stdin",
			stdout: "/test/stdout",
			stderr: "/test/stderr",
		},
		"empty stdio": {},
	} {
		t.Logf("TestCase %q", desc)
		c := newTestCRIContainerdService()
		fakeOS := c.os.(*ostesting.FakeOS)
		fakeOS.OpenFifoFn = func(ctx context.Context, fn string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
			expectFlag := syscall.O_RDONLY | syscall.O_CREAT | syscall.O_NONBLOCK
			if fn == test.stdin {
				expectFlag = syscall.O_WRONLY | syscall.O_CREAT | syscall.O_NONBLOCK
			}
			assert.Equal(t, expectFlag, flag)
			assert.Equal(t, os.FileMode(0700), perm)
			return nopReadWriteCloser{}, nil
		}
		i, o, e, err := c.prepareStreamingPipes(context.Background(), test.stdin, test.stdout, test.stderr)
		assert.NoError(t, err)
		assert.Equal(t, test.stdin != "", i != nil)
		assert.Equal(t, test.stdout != "", o != nil)
		assert.Equal(t, test.stderr != "", e != nil)
	}
}

type closeTestReadWriteCloser struct {
	CloseFn func() error
	nopReadWriteCloser
}

func (c closeTestReadWriteCloser) Close() error {
	return c.CloseFn()
}

func TestPrepareStreamingPipesError(t *testing.T) {
	stdin, stdout, stderr := "/test/stdin", "/test/stdout", "/test/stderr"
	for desc, inject := range map[string]map[string]error{
		"should cleanup on stdin error":  {stdin: fmt.Errorf("stdin error")},
		"should cleanup on stdout error": {stdout: fmt.Errorf("stdout error")},
		"should cleanup on stderr error": {stderr: fmt.Errorf("stderr error")},
	} {
		t.Logf("TestCase %q", desc)
		c := newTestCRIContainerdService()
		fakeOS := c.os.(*ostesting.FakeOS)
		openFlags := map[string]bool{
			stdin:  false,
			stdout: false,
			stderr: false,
		}
		fakeOS.OpenFifoFn = func(ctx context.Context, fn string, flag int, perm os.FileMode) (io.ReadWriteCloser, error) {
			if inject[fn] != nil {
				return nil, inject[fn]
			}
			openFlags[fn] = !openFlags[fn]
			testCloser := closeTestReadWriteCloser{}
			testCloser.CloseFn = func() error {
				openFlags[fn] = !openFlags[fn]
				return nil
			}
			return testCloser, nil
		}
		i, o, e, err := c.prepareStreamingPipes(context.Background(), stdin, stdout, stderr)
		assert.Error(t, err)
		assert.Nil(t, i)
		assert.Nil(t, o)
		assert.Nil(t, e)
		assert.False(t, openFlags[stdin])
		assert.False(t, openFlags[stdout])
		assert.False(t, openFlags[stderr])
	}
}

func TestGetSandbox(t *testing.T) {
	c := newTestCRIContainerdService()
	testID := "abcdefg"
	testSandbox := metadata.SandboxMetadata{
		ID:   testID,
		Name: "test-name",
	}
	assert.NoError(t, c.sandboxStore.Create(testSandbox))
	assert.NoError(t, c.sandboxIDIndex.Add(testID))

	for desc, test := range map[string]struct {
		id        string
		expected  *metadata.SandboxMetadata
		expectErr bool
	}{
		"full id": {
			id:        testID,
			expected:  &testSandbox,
			expectErr: false,
		},
		"partial id": {
			id:        testID[:3],
			expected:  &testSandbox,
			expectErr: false,
		},
		"non-exist id": {
			id:        "gfedcba",
			expected:  nil,
			expectErr: true,
		},
	} {
		t.Logf("TestCase %q", desc)
		sb, err := c.getSandbox(test.id)
		if test.expectErr {
			assert.Error(t, err)
			assert.True(t, metadata.IsNotExistError(err))
		} else {
			assert.NoError(t, err)
		}
		assert.Equal(t, test.expected, sb)
	}
}
