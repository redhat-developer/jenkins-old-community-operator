/*
Copyright 2016 The Kubernetes Authors.

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
//nolint: gocritic,unparam
package util

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/kubectl/pkg/cmd/exec"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// CopyOptions have the data required to perform the copy operation
type CopyOptions struct {
	Container  string
	Namespace  string
	NoPreserve bool

	ClientConfig      *restclient.Config
	K8sclient         kubernetes.Clientset
	ExecParentCmdName string

	genericclioptions.IOStreams
}

// NewCopyOptions creates the options for copy
func NewCopyOptions(clientConfig *restclient.Config, clientSet kubernetes.Clientset, ioStreams genericclioptions.IOStreams) *CopyOptions {
	return &CopyOptions{
		ClientConfig: clientConfig,
		K8sclient:    clientSet,
		IOStreams:    ioStreams,
	}
}

type fileSpec struct {
	PodNamespace string
	PodName      string
	File         string
}

var (
	errFileSpecDoesntMatchFormat = errors.New("filespec must match the canonical format: [[namespace/]pod:]file/path")
	errFileCannotBeEmpty         = errors.New("filepath can not be empty")
)

func extractFileSpec(arg string) (fileSpec, error) {
	if i := strings.Index(arg, ":"); i == -1 {
		return fileSpec{File: arg}, nil
	} else if i > 0 {
		file := arg[i+1:]
		pod := arg[:i]
		pieces := strings.Split(pod, "/")
		if len(pieces) == 1 {
			return fileSpec{
				PodName: pieces[0],
				File:    file,
			}, nil
		}
		if len(pieces) == 2 {
			return fileSpec{
				PodNamespace: pieces[0],
				PodName:      pieces[1],
				File:         file,
			}, nil
		}
	}

	return fileSpec{}, errFileSpecDoesntMatchFormat
}

// Run performs the execution
func (o *CopyOptions) Run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("source and destination are required")
	}
	srcSpec, err := extractFileSpec(args[0])
	if err != nil {
		return err
	}

	destSpec, err := extractFileSpec(args[1])
	if err != nil {
		return err
	}
	if len(srcSpec.PodName) != 0 && len(destSpec.PodName) != 0 {
		if _, err := os.Stat(args[0]); err == nil {
			return o.copyToPod(fileSpec{File: args[0]}, destSpec, &exec.ExecOptions{})
		}
		return fmt.Errorf("src doesn't exist in local filesystem")
	}

	if len(srcSpec.PodName) != 0 {
		return o.copyFromPod(srcSpec, destSpec)
	}
	if len(destSpec.PodName) != 0 {
		return o.copyToPod(srcSpec, destSpec, &exec.ExecOptions{})
	}
	return fmt.Errorf("one of src or dest must be a remote file specification")
}

// checkDestinationIsDir receives a destination fileSpec and
// determines if the provided destination path exists on the
// pod. If the destination path does not exist or is _not_ a
// directory, an error is returned with the exit code received.
func (o *CopyOptions) checkDestinationIsDir(dest fileSpec) error {
	options := &exec.ExecOptions{
		StreamOptions: exec.StreamOptions{
			IOStreams: genericclioptions.IOStreams{
				Out:    bytes.NewBuffer([]byte{}),
				ErrOut: bytes.NewBuffer([]byte{}),
			},

			Namespace: dest.PodNamespace,
			PodName:   dest.PodName,
		},

		Command:  []string{"test", "-d", dest.File},
		Executor: &exec.DefaultRemoteExecutor{},
	}

	return o.execute(options)
}

func (o *CopyOptions) copyToPod(src, dest fileSpec, options *exec.ExecOptions) error {
	if len(src.File) == 0 || len(dest.File) == 0 {
		return errFileCannotBeEmpty
	}

	reader, writer := io.Pipe()

	// strip trailing slash (if any)
	if dest.File != "/" && strings.HasSuffix(string(dest.File[len(dest.File)-1]), "/") {
		dest.File = dest.File[:len(dest.File)-1]
	}

	if err := o.checkDestinationIsDir(dest); err == nil {
		// If no error, dest.File was found to be a directory.
		// Copy specified src into it
		dest.File = dest.File + "/" + path.Base(src.File)
	}

	go func() {
		defer writer.Close()
		err := makeTar(src.File, dest.File, writer)
		cmdutil.CheckErr(err)
	}()
	var cmdArr []string

	// TODO: Improve error messages by first testing if 'tar' is present in the container?
	if o.NoPreserve {
		cmdArr = []string{"tar", "--no-same-permissions", "--no-same-owner", "-xmf", "-"}
	} else {
		cmdArr = []string{"tar", "-xmf", "-"}
	}
	destDir := path.Dir(dest.File)
	if len(destDir) > 0 {
		cmdArr = append(cmdArr, "-C", destDir)
	}

	options.StreamOptions = exec.StreamOptions{
		IOStreams: genericclioptions.IOStreams{
			In:     reader,
			Out:    o.Out,
			ErrOut: o.ErrOut,
		},
		Stdin: true,

		Namespace: dest.PodNamespace,
		PodName:   dest.PodName,
	}

	options.Command = cmdArr
	options.Executor = &exec.DefaultRemoteExecutor{}
	return o.execute(options)
}

func (o *CopyOptions) copyFromPod(src, dest fileSpec) error {
	if len(src.File) == 0 || len(dest.File) == 0 {
		return errFileCannotBeEmpty
	}

	reader, outStream := io.Pipe()
	options := &exec.ExecOptions{
		StreamOptions: exec.StreamOptions{
			IOStreams: genericclioptions.IOStreams{
				In:     nil,
				Out:    outStream,
				ErrOut: o.Out,
			},

			Namespace: src.PodNamespace,
			PodName:   src.PodName,
		},

		// TODO: Improve error messages by first testing if 'tar' is present in the container?
		Command:  []string{"tar", "cf", "-", src.File},
		Executor: &exec.DefaultRemoteExecutor{},
	}

	go func() {
		defer outStream.Close()
		err := o.execute(options)
		cmdutil.CheckErr(err)
	}()
	prefix := getPrefix(src.File)
	prefix = path.Clean(prefix)
	// remove extraneous path shortcuts - these could occur if a path contained extra "../"
	// and attempted to navigate beyond "/" in a remote filesystem
	prefix = stripPathShortcuts(prefix)
	return o.untarAll(src, reader, dest.File, prefix)
}

// stripPathShortcuts removes any leading or trailing "../" from a given path
func stripPathShortcuts(p string) string {
	newPath := path.Clean(p)
	trimmed := strings.TrimPrefix(newPath, "../")

	for trimmed != newPath {
		newPath = trimmed
		trimmed = strings.TrimPrefix(newPath, "../")
	}

	// trim leftover {".", ".."}
	if newPath == "." || newPath == ".." {
		newPath = ""
	}

	if len(newPath) > 0 && string(newPath[0]) == "/" {
		return newPath[1:]
	}

	return newPath
}

func makeTar(srcPath, destPath string, writer io.Writer) error {
	// TODO: use compression here?
	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	srcPath = path.Clean(srcPath)
	destPath = path.Clean(destPath)
	return recursiveTar(path.Dir(srcPath), path.Base(srcPath), path.Dir(destPath), path.Base(destPath), tarWriter)
}

func recursiveTar(srcBase, srcFile, destBase, destFile string, tw *tar.Writer) error {
	srcPath := path.Join(srcBase, srcFile)
	matchedPaths, err := filepath.Glob(srcPath)
	if err != nil {
		return err
	}
	for _, fpath := range matchedPaths {
		stat, err := os.Lstat(fpath)
		if err != nil {
			return err
		}
		if stat.IsDir() {
			files, err := ioutil.ReadDir(fpath)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				//case empty directory
				hdr, _ := tar.FileInfoHeader(stat, fpath)
				hdr.Name = destFile
				if err := tw.WriteHeader(hdr); err != nil {
					return err
				}
			}
			for _, f := range files {
				if err := recursiveTar(srcBase, path.Join(srcFile, f.Name()), destBase, path.Join(destFile, f.Name()), tw); err != nil {
					return err
				}
			}
			return nil
		} else if stat.Mode()&os.ModeSymlink != 0 {
			//case soft link
			hdr, _ := tar.FileInfoHeader(stat, fpath)
			target, err := os.Readlink(fpath)
			if err != nil {
				return err
			}

			hdr.Linkname = target
			hdr.Name = destFile
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
		} else {
			//case regular file or other file type like pipe
			hdr, err := tar.FileInfoHeader(stat, fpath)
			if err != nil {
				return err
			}
			hdr.Name = destFile

			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}

			f, err := os.Open(fpath)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
			return f.Close()
		}
	}
	return nil
}

func (o *CopyOptions) untarAll(src fileSpec, reader io.Reader, destDir, prefix string) error {
	symlinkWarningPrinted := false
	// TODO: use compression here?
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}

		// All the files will start with the prefix, which is the directory where
		// they were located on the pod, we need to strip down that prefix, but
		// if the prefix is missing it means the tar was tempered with.
		// For the case where prefix is empty we need to ensure that the path
		// is not absolute, which also indicates the tar file was tempered with.
		if !strings.HasPrefix(header.Name, prefix) {
			return fmt.Errorf("tar contents corrupted")
		}

		// basic file information
		mode := header.FileInfo().Mode()
		destFileName := filepath.Join(destDir, header.Name[len(prefix):])

		if !isDestRelative(destDir, destFileName) {
			fmt.Fprintf(o.IOStreams.ErrOut, "warning: file %q is outside target destination, skipping\n", destFileName)
			continue
		}

		baseName := filepath.Dir(destFileName)
		if err := os.MkdirAll(baseName, 0755); err != nil {
			return err
		}
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(destFileName, 0755); err != nil {
				return err
			}
			continue
		}

		if mode&os.ModeSymlink != 0 {
			if !symlinkWarningPrinted && len(o.ExecParentCmdName) > 0 {
				fmt.Fprintf(o.IOStreams.ErrOut, "warning: skipping symlink: %q -> %q (consider using \"%s exec -n %q %q -- tar cf - %q | tar xf -\")\n", destFileName, header.Linkname, o.ExecParentCmdName, src.PodNamespace, src.PodName, src.File)
				symlinkWarningPrinted = true
				continue
			}
			fmt.Fprintf(o.IOStreams.ErrOut, "warning: skipping symlink: %q -> %q\n", destFileName, header.Linkname)
			continue
		}
		outFile, err := os.Create(destFileName)
		if err != nil {
			return err
		}
		defer outFile.Close()
		if _, err := io.Copy(outFile, tarReader); err != nil {
			return err
		}
		if err := outFile.Close(); err != nil {
			return err
		}
	}

	return nil
}

// isDestRelative returns true if dest is pointing outside the base directory,
// false otherwise.
func isDestRelative(base, dest string) bool {
	relative, err := filepath.Rel(base, dest)
	if err != nil {
		return false
	}
	return relative == "." || relative == stripPathShortcuts(relative)
}

func getPrefix(file string) string {
	// tar strips the leading '/' if it's there, so we will too
	return strings.TrimLeft(file, "/")
}

func (o *CopyOptions) execute(options *exec.ExecOptions) error {
	if len(options.Namespace) == 0 {
		options.Namespace = o.Namespace
	}

	if len(o.Container) > 0 {
		options.ContainerName = o.Container
	}

	options.Config = o.ClientConfig
	options.PodClient = o.K8sclient.CoreV1()

	if err := options.Run(); err != nil {
		return err
	}
	return nil
}
