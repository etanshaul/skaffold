/*
Copyright 2019 The Skaffold Authors

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

package runner

import (
	"context"
	"io"
	"time"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/build"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/build/tag"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/schema/latest"
	"github.com/pkg/errors"
)

// BuildAndTest builds and tests a list of artifacts.
func (r *SkaffoldRunner) BuildAndTest(ctx context.Context, out io.Writer, artifacts []*latest.Artifact) ([]build.Artifact, error) {
	tags, err := r.imageTags(ctx, out, artifacts)
	if err != nil {
		return nil, errors.Wrap(err, "generating tag")
	}

	bRes, err := r.cache.Build(ctx, out, tags, artifacts, func(ctx context.Context, out io.Writer, tags tag.ImageTags, artifacts []*latest.Artifact) ([]build.Artifact, error) {
		if len(artifacts) == 0 {
			return nil, nil
		}

		r.hasBuilt = true

		bRes, err := r.Builder.Build(ctx, out, tags, artifacts)
		if err != nil {
			return nil, errors.Wrap(err, "build failed")
		}

		if !r.runCtx.Opts.SkipTests {
			if err = r.Tester.Test(ctx, out, bRes); err != nil {
				return nil, errors.Wrap(err, "test failed")
			}
		}

		return bRes, nil
	})
	if err != nil {
		return nil, err
	}

	// Update which images are logged.
	for _, build := range bRes {
		r.imageList.Add(build.Tag)
	}

	// Make sure all artifacts are redeployed. Not only those that were just built.
	r.builds = build.MergeWithPreviousBuilds(bRes, r.builds)
	return bRes, nil
}

// DeployAndLog deploys a list of already built artifacts and optionally show the logs.
func (r *SkaffoldRunner) DeployAndLog(ctx context.Context, out io.Writer, artifacts []build.Artifact) error {
	if !r.runCtx.Opts.Tail {
		return r.Deploy(ctx, out, artifacts)
	}

	var imageNames []string
	for _, artifact := range artifacts {
		imageNames = append(imageNames, artifact.ImageName)
	}

	logger := r.newLoggerForImages(out, imageNames)
	defer logger.Stop()

	// Logs should be retrieve up to just before the deploy
	logger.SetSince(time.Now())

	if err := r.Deploy(ctx, out, artifacts); err != nil {
		return err
	}

	// Start printing the logs after deploy is finished
	if err := logger.Start(ctx); err != nil {
		return errors.Wrap(err, "starting logger")
	}

	<-ctx.Done()

	return nil
}
