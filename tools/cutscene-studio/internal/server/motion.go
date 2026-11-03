package server

import (
	"encoding/binary"
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"

	"mgs2-restoration/cutscene-studio/internal/model"
	"mgs2-restoration/cutscene-studio/internal/pose"
	"mgs2-restoration/cutscene-studio/internal/scene"
)

// handleMotion streams one object's skeletal animation, already posed onto the model's
// skeleton, so the client does no matrix maths -- it just assigns each bone.
//
// Binary, not JSON: a 53-joint character over 1,400 frames is ~500k floats, which is a few
// megabytes as raw f32 and tens of megabytes as JSON text.
//
//	u32 frames
//	u32 bones
//	f32[frames][bones][7]   quat x,y,z,w then trans x,y,z
//
// The bone count is the SKELETON's, not the demo's: `n_joints` is the model's base rig and
// can be smaller than the skeleton the engine walks (amg_def is 3 vs 5). The tail holds its
// rest pose, which is what the engine does by clearing to an identity quaternion.
func (s *Server) handleMotion(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	objID, err := strconv.Atoi(r.URL.Query().Get("object"))
	if path == "" || err != nil {
		fail(w, 400, fmt.Errorf("path and object are required"))
		return
	}

	sc, err := s.motionScene(path)
	if err != nil {
		fail(w, 500, err)
		return
	}

	var obj *scene.ObjectTrack
	for _, o := range sc.Objects {
		if int(o.ID) == objID {
			obj = o
			break
		}
	}
	if obj == nil {
		fail(w, 404, fmt.Errorf("no object %d", objID))
		return
	}
	if len(obj.Joints) == 0 {
		fail(w, 404, fmt.Errorf("object %d carries no joint animation", objID))
		return
	}

	mesh, err := s.meshOf(obj)
	if err != nil {
		fail(w, 404, err)
		return
	}
	sk, err := pose.SkeletonOf(mesh)
	if err != nil {
		fail(w, 500, err)
		return
	}

	n := sc.Frames
	nb := sk.Len()
	out := make([]byte, 8+n*nb*7*4)
	binary.LittleEndian.PutUint32(out[0:], uint32(n))
	binary.LittleEndian.PutUint32(out[4:], uint32(nb))

	js := make([]pose.Joint, 0, 64)
	o := 8
	for f := 0; f < n; f++ {
		src := obj.Joints[f]
		js = js[:0]
		for _, j := range src {
			js = append(js, pose.Joint{Quat: j.Quat, Trans: j.Trans})
		}
		bones, err := pose.Pose(sk, js)
		if err != nil {
			fail(w, 500, fmt.Errorf("frame %d: %w", f, err))
			return
		}
		for _, b := range bones {
			for _, v := range [...]float32{b.Quat[0], b.Quat[1], b.Quat[2], b.Quat[3], b.Trans[0], b.Trans[1], b.Trans[2]} {
				binary.LittleEndian.PutUint32(out[o:], math.Float32bits(v))
				o += 4
			}
		}
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(out)
}

// meshOf picks the mesh the engine actually skins: the EVM when the object has one, the KMS
// otherwise. cache_id is 0 on 79% of objects (rigid props are KMS-only and legal).
func (s *Server) meshOf(o *scene.ObjectTrack) (*model.Mesh, error) {
	ix, err := s.index()
	if err != nil {
		return nil, err
	}
	id, ext := o.KMSCacheID, "kms"
	if o.CacheID != 0 {
		id, ext = o.CacheID, "evm"
	}
	cid, _ := model.SplitCacheID(id)
	for _, a := range ix.Resolve(cid, ext) {
		if !ix.Exists(a) {
			continue
		}
		if m, err := model.LoadModel(ix.FilePath(a)); err == nil {
			return m, nil
		}
	}
	return nil, fmt.Errorf("no %s mesh for cache id 0x%06x", ext, cid)
}

// motionScene keeps a second copy of the demo imported WITH joints. The default import
// drops them: retaining every joint of every frame costs gigabytes on the big demos, and
// only skeletal editing and this endpoint ever need them.
func (s *Server) motionScene(path string) (*scene.Scene, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sc, ok := s.motion[canon(path)]; ok {
		return sc, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	sc, err := scene.ImportWith(path, raw, s.cat, scene.Opts{Joints: true})
	if err != nil {
		return nil, err
	}
	if s.motion == nil {
		s.motion = map[string]*scene.Scene{}
	}
	s.motion[canon(path)] = sc
	return sc, nil
}
