package fs

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/go-fsnotify/fsnotify"
	"github.com/influx6/assets"
	"github.com/influx6/flux"
)

// WatchConfig provides configuration for the WatchDir and WatchFile tasks
type WatchConfig struct {
	Path      string
	Validator assets.PathValidator
	Mux       assets.PathMux
}

// Watch returns a task handler that watches a path for changes and passes down the file which changed
func Watch(m WatchConfig) flux.Reactor {
	var running bool
	mo := flux.Reactive(func(root flux.Reactor, err error, _ interface{}) {
		if err != nil {
			root.ReplyError(err)
			return
		}

		if running {
			return
		}

		stat, err := os.Stat(m.Path)

		if err != nil {
			root.ReplyError(err)
			go root.Close()
			return
		}

		running = true

		if !stat.IsDir() {
			flux.GoDefer("Watch", func() {
				defer root.Close()

				for {

					wo, err := fsnotify.NewWatcher()

					if err != nil {
						root.ReplyError(err)
						break
					}

					if err := wo.Add(m.Path); err != nil {
						wo.Close()
						break
					}

					select {
					case ev, ok := <-wo.Events:
						if ok {
							root.Reply(ev)
						}
					case erx, ok := <-wo.Errors:
						if ok {
							root.ReplyError(erx)
						}
					case <-root.CloseNotify():
						wo.Close()
						break
					}

					wo.Close()
				}
			})

			return
		}

		dir, err := assets.DirListings(m.Path, m.Validator, m.Mux)

		if err != nil {
			root.ReplyError(err)
			go root.Close()
			return
		}

		flux.GoDefer("Watch", func() {
			defer root.Close()

			for {

				wo, err := fsnotify.NewWatcher()

				if err != nil {
					root.ReplyError(err)
					break
				}

				dir.Listings.Wo.RLock()
				for _, files := range dir.Listings.Tree {
					wo.Add(files.AbsDir)
					files.Tree.Each(func(mod, real string) {
						rel, _ := filepath.Abs(real)
						wo.Add(rel)
						// wo.Add(filepath.Join(files.AbsDir, real))
					})
				}
				dir.Listings.Wo.RUnlock()

				select {
				case <-root.CloseNotify():
					wo.Close()
					break
				case ev, ok := <-wo.Events:
					if ok {
						file := filepath.Clean(ev.Name)
						// stat, _ := os.Stat(file)
						if (&m).Validator != nil {
							if (&m).Validator(file, nil) {
								root.Reply(ev)
							}
						} else {
							root.Reply(ev)
						}
					}
				case erx, ok := <-wo.Errors:
					if ok {
						root.ReplyError(erx)
					}
				}

				wo.Close()

				if err = dir.Reload(); err != nil {
					root.ReplyError(err)
				}

			}
		})

	})

	mo.Send(true)
	return mo
}

// WatchSetConfig provides configuration for using the WatchSet watcher tasks
type WatchSetConfig struct {
	Path      []string
	Validator assets.PathValidator
	Mux       assets.PathMux
}

// WatchSet unlike Watch is not set for only working with one directory, by providing a WatchSetConfig you can supply multiple directories and files which will be sorted and watch if all paths were found to be invalid then the watcher will be closed and so will the task, an invalid file error will be forwarded down the reactor chain
func WatchSet(m WatchSetConfig) flux.Reactor {
	var running bool
	mo := flux.Reactive(func(root flux.Reactor, err error, _ interface{}) {
		if err != nil {
			root.ReplyError(err)
			return
		}

		if running {
			return
		}

		running = true

		var dirlistings []*assets.DirListing
		var files []string
		var dirsAdded = make(map[string]bool)

		for _, path := range m.Path {
			if dirsAdded[path] {
				continue
			}

			stat, err := os.Stat(path)
			if err != nil {
				// log.Printf("stat error: %s", err)
				root.ReplyError(err)
				continue
			}

			if stat.IsDir() {
				if dir, err := assets.DirListings(path, m.Validator, m.Mux); err == nil {
					dirsAdded[path] = true
					dirlistings = append(dirlistings, dir)
				} else {
					root.ReplyError(err)
				}
			} else {
				if !dirsAdded[filepath.Dir(path)] {
					files = append(files, path)
				}
			}
		}

		if len(dirlistings) <= 0 && len(files) <= 0 {
			log.Printf("no dirlistings, will close")
			go root.Close()
			log.Printf("no dirlistings, will close")
			return
		}

		flux.GoDefer("Watch", func() {
			defer root.Close()

			for {

				wo, err := fsnotify.NewWatcher()

				if err != nil {
					root.ReplyError(err)
					break
				}

				// var watched = make(map[string]bool)
				//reload all concerned directories into watcher
				for _, dir := range dirlistings {
					dir.Listings.Wo.RLock()
					for _, files := range dir.Listings.Tree {
						// log.Printf("Checking folder: %s", files.Dir)
						// if !watched[files.AbsDir] {
						// watched[files.AbsDir] = true
						wo.Add(files.AbsDir)
						// }

						files.Tree.Each(func(mod, real string) {
							// if watched[real] {
							// log.Printf("duplicate found %s -> %s -> %s", mod, real, files.AbsDir)
							// return
							// }

							// watched[real] = true
							rel, _ := filepath.Abs(real)
							wo.Add(rel)
							// if err != nil {
							// 	rel = real
							// }
							// wo.Add(filepath.Join(files.Dir, real))
							// wo.Add(filepath.Join(files.AbsDir, real))
						})
					}
					dir.Listings.Wo.RUnlock()
				}

				//reload all concerned files found in the path
				for _, file := range files {
					wo.Add(file)
				}

				select {
				case <-root.CloseNotify():
					break
				case ev, ok := <-wo.Events:
					if ok {
						if (&m).Validator != nil {
							file := filepath.Clean(ev.Name)
							// log.Printf("checking file: %s", file)
							if (&m).Validator(file, nil) {
								// log.Printf("passed file: %s", file)
								root.Reply(ev)
							}
						} else {
							// log.Printf("backdrop file: %s", ev)
							root.Reply(ev)
						}
					}
				case erx, ok := <-wo.Errors:
					if ok {
						root.ReplyError(erx)
					}
				}

				wo.Close()

				//reload all concerned directories
				for _, dir := range dirlistings {
					dir.Reload()
				}
			}
		})

	})

	mo.Send(true)
	return mo
}

// ModFileRead provides a task that allows building a FileRead modder,where you mod out the values for a particular FileRead struct
func ModFileRead(fx func(*FileRead)) flux.Reactor {
	return flux.Reactive(flux.SimpleMuxer(func(root flux.Reactor, data interface{}) {
		if fw, ok := data.(*FileRead); ok {
			fx(fw)
			root.Reply(fw)
		}
	}))
}

// ModFileWrite provides a task that allows building a fileWrite modder,where you mod out the values for a particular FileWrite struct
func ModFileWrite(fx func(*FileWrite)) flux.Reactor {
	return flux.Reactive(flux.SimpleMuxer(func(root flux.Reactor, data interface{}) {
		if fw, ok := data.(*FileWrite); ok {
			fx(fw)
			root.Reply(fw)
		}
	}))
}

// FileRead represents an output from Read Tasks
type FileRead struct {
	Data []byte
	Path string
}

// FileReader returns a new flux.Reactor that takes a path and reads out returning the file path
func FileReader() flux.Reactor {
	return flux.Reactive(flux.SimpleMuxer(func(root flux.Reactor, data interface{}) {
		if pr, ok := data.(*FileRead); ok {
			root.Reply(pr)
			return
		}

		if path, ok := data.(string); ok {
			if _, err := os.Stat(path); err == nil {
				file, err := os.Open(path)

				if err != nil {
					root.ReplyError(err)
					return
				}

				defer file.Close()

				var buf bytes.Buffer

				//copy over data
				_, err = io.Copy(&buf, file)

				//if we have an error and its not EOF then reply with error
				if err != nil && err != io.EOF {
					root.ReplyError(err)
					return
				}

				root.Reply(&FileRead{Data: buf.Bytes(), Path: path})
			} else {
				root.ReplyError(err)
			}
		}
	}))
}

// ErrInvalidPath is returned when the path in the FileWrite is empty
var ErrInvalidPath = errors.New("FileWrite has an empty path,which is invalid")

var defaultMux = func(s string) string { return s }

// FileWrite represents an output from Write Tasks
type FileWrite struct {
	Data []byte
	Path string
}

// FileWriter takes the giving data of type FileWriter and writes the value out into a endpoint which is the value of Path in the FileWriter struct, it takes an optional function which reforms the path to save the file
func FileWriter(fx func(string) string) flux.Reactor {
	if fx == nil {
		fx = defaultMux
	}
	return flux.Reactive(flux.SimpleMuxer(func(root flux.Reactor, data interface{}) {
		if file, ok := data.(*FileWrite); ok {
			if file.Path == "" {
				root.ReplyError(ErrInvalidPath)
				return
			}
			// endpoint := filepath.Join(toPath, file.Path)

			endpoint := fx(file.Path)
			endpointDir := filepath.Dir(endpoint)

			//make the directory part incase it does not exists
			os.MkdirAll(endpointDir, 0700)

			osfile, err := os.Create(endpoint)

			if err != nil {
				root.ReplyError(err)
				return
			}

			defer osfile.Close()

			// io.Copy(osfile, file.Data)
			osfile.Write(file.Data)

			root.Reply(&FileWrite{Path: endpoint})
		}
	}))
}

// FileOpCopy listens for either a FilRead or FileWrite and send that off to a given set of reactors, to reduce memory footprint the FilRead/FileWrite pointer is sent as is, so if you want a fresh copy, dereference it to have a unique copy
func FileOpCopy(to ...flux.Reactor) flux.Reactor {
	return flux.Reactive((func(root flux.Reactor, err error, data interface{}) {
		if err != nil {
			for _, fx := range to {
				fx.SendError(err)
			}
			return
		}

		if file, ok := data.(*FileWrite); ok {
			for _, fx := range to {
				fx.Send(file)
			}
		}

		if file, ok := data.(*FileRead); ok {
			for _, fx := range to {
				fx.Send(file)
			}
		}
	}))
}

// FileAppender takes the giving data of type FileWriter and appends the value out into a endpoint which is the combination of the name and the toPath value provided
func FileAppender(fx func(string) string) flux.Reactor {
	if fx == nil {
		fx = defaultMux
	}
	return flux.Reactive(flux.SimpleMuxer(func(root flux.Reactor, data interface{}) {
		if file, ok := data.(*FileWrite); ok {
			// endpoint := filepath.Join(toPath, file.Path)

			endpoint := fx(file.Path)
			endpointDir := filepath.Dir(endpoint)

			//make the directory part incase it does not exists
			os.MkdirAll(endpointDir, 0700)

			osfile, err := os.Open(endpoint)

			if err != nil {
				root.ReplyError(err)
				return
			}

			defer osfile.Close()

			// io.Copy(osfile, file.Data)

			osfile.Write(file.Data)
			root.Reply(&FileWrite{Path: endpoint})
		}
	}))
}

// RemoveFile represents a file to be removed by a FileRemover task
type RemoveFile struct {
	Path string
}

// FileRemover takes a *RemoveFile as the data and removes the path giving by the RemoveFile.Path, to remove all path along using os.Remove, use the FileAllRemover
func FileRemover() flux.Reactor {
	return flux.Reactive(flux.SimpleMuxer(func(root flux.Reactor, data interface{}) {
		if file, ok := data.(*RemoveFile); ok {
			err := os.Remove(file.Path)

			if err != nil {
				root.ReplyError(err)
				return
			}
		}
	}))
}

// FileAllRemover takes a *RemoveFile as the data and removes the path using the os.RemoveAll
func FileAllRemover() flux.Reactor {
	return flux.Reactive(flux.SimpleMuxer(func(root flux.Reactor, data interface{}) {
		if file, ok := data.(*RemoveFile); ok {
			err := os.RemoveAll(file.Path)

			if err != nil {
				root.ReplyError(err)
				return
			}
		}
	}))
}

// ListingConfig provides configuration for the StreamListings task
type ListingConfig struct {
	Path        string
	DirAlso     bool // optional: if true, will list dir also as part of the listing stream else skip
	UseRelative bool // optional: if true, will only list in relative paths
	Validator   assets.PathValidator
	Mux         assets.PathMux
}

// StreamListings takes a path and generates a assets.DirListing struct when it receives any signal, it will go through all the files within each listings.
func StreamListings(config ListingConfig) (flux.Reactor, error) {
	dir, err := assets.DirListings(config.Path, config.Validator, config.Mux)

	if err != nil {
		return nil, err
	}

	return flux.FlatSimple(func(root flux.Reactor, data interface{}) {
		if err := dir.Reload(); err != nil {
			root.ReplyError(err)
			return
		}

		// no error occured reloading, so we stream out the directory, list
		dir.Listings.Wo.RLock()
		for _, files := range dir.Listings.Tree {
			if config.DirAlso {
				if !config.UseRelative {
					root.Reply(files.AbsDir)
				} else {
					root.Reply(filepath.ToSlash(files.Dir))
				}
			}
			files.Tree.Each(func(mod, real string) {
				if !config.UseRelative {
					rel, err := filepath.Abs(real)
					if err != nil {
						rel = real
					}
					// log.Printf("Sending %s -> %s -> %s", files.AbsDir, real, rel)
					root.Reply(rel)
				} else {
					root.Reply(filepath.Join(files.Dir, real))
				}
			})
		}
		dir.Listings.Wo.RUnlock()

	}), nil
}
