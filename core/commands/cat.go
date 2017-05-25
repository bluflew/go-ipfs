package commands

import (
	"context"
	"io"
	"os"

	core "github.com/ipfs/go-ipfs/core"
	coreunix "github.com/ipfs/go-ipfs/core/coreunix"

	"gx/ipfs/QmWdiBLZ22juGtuNceNbvvHV11zKzCaoQFMP76x2w1XDFZ/go-ipfs-cmdkit"
	cmds "gx/ipfs/QmZro8GXyJpJWtjrrSEr78dBdkZQ8ZnNjoCNB9FLEQWyRt/go-ipfs-cmds"
)

const progressBarMinSize = 1024 * 1024 * 8 // show progress bar for outputs > 8MiB

var CatCmd = &cmds.Command{
	Helptext: cmdsutil.HelpText{
		Tagline:          "Show IPFS object data.",
		ShortDescription: "Displays the data contained by an IPFS or IPNS object(s) at the given path.",
	},

	Arguments: []cmdsutil.Argument{
		cmdsutil.StringArg("ipfs-path", true, true, "The path to the IPFS object(s) to be outputted.").EnableStdin(),
	},
	Run: func(req cmds.Request, re cmds.ResponseEmitter) {
		node, err := req.InvocContext().GetNode()
		if err != nil {
			re.SetError(err, cmdsutil.ErrNormal)
			return
		}

		if !node.OnlineMode() {
			if err := node.SetupOfflineRouting(); err != nil {
				re.SetError(err, cmdsutil.ErrNormal)
				return
			}
		}

		readers, length, err := cat(req.Context(), node, req.Arguments())
		if err != nil {
			re.SetError(err, cmdsutil.ErrNormal)
			return
		}

		/*
			if err := corerepo.ConditionalGC(req.Context(), node, length); err != nil {
				re.SetError(err, cmdsutil.ErrNormal)
				return
			}
		*/

		re.SetLength(length)
		reader := io.MultiReader(readers...)

		// Since the reader returns the error that a block is missing, and that error is
		// returned from io.Copy inside Emit, we need to take Emit errors and send
		// them to the client. Usually we don't do that because it means the connection
		// is broken or we supplied an illegal argument etc.
		err = re.Emit(reader)
		if err != nil {
			re.SetError(err, cmdsutil.ErrNormal)
		}
	},
	PostRun: map[cmds.EncodingType]func(cmds.Request, cmds.ResponseEmitter) cmds.ResponseEmitter{
		cmds.CLI: func(req cmds.Request, re cmds.ResponseEmitter) cmds.ResponseEmitter {
			reNext, res := cmds.NewChanResponsePair(req)

			go func() {
				if res.Length() > 0 && res.Length() < progressBarMinSize {
					if err := cmds.Copy(re, res); err != nil {
						re.SetError(err, cmdsutil.ErrNormal)
					}

					return
				}

				// Copy closes by itself, so we must not do this before
				defer re.Close()
				for {
					v, err := res.Next()
					if err == io.EOF {
						break
					} else if err != nil {
						if err == cmds.ErrRcvdError {
							re.Emit(res.Error())
						} else {
							re.SetError(err, cmdsutil.ErrNormal)
						}
					}

					switch val := v.(type) {
					case *cmdsutil.Error:
						re.Emit(val)
					case io.Reader:
						bar, reader := progressBarForReader(os.Stderr, val, int64(res.Length()))
						bar.Start()

						err = re.Emit(reader)
						if err != nil {
							log.Error(err)
						}
					default:
						log.Warningf("cat postrun: received unexpected type %T", val)
					}
				}
			}()

			return reNext
		},
	},
}

func cat(ctx context.Context, node *core.IpfsNode, paths []string) ([]io.Reader, uint64, error) {
	readers := make([]io.Reader, 0, len(paths))
	length := uint64(0)
	for _, fpath := range paths {
		read, err := coreunix.Cat(ctx, node, fpath)
		if err != nil {
			return nil, 0, err
		}
		readers = append(readers, read)
		length += uint64(read.Size())
	}
	return readers, length, nil
}
