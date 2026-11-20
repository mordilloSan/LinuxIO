package dockerupdate

// measuredBuildOutputs is what `docker compose build --pull --progress=plain`
// wrote, verbatim, with BUILDX_NO_DEFAULT_ATTESTATIONS=1, for a service whose
// Dockerfile was each case, on Docker 29.1.3 with the containerd image store
// and Compose 2.40.3 without buildx (the test VM, November 2026). The first
// build's RUN step prints two lines made to look like base images; the second
// carried a carriage return after its prefix, which a raw string cannot hold,
// so the spoofing test adds it back.
var measuredBuildOutputs = map[string]string{
	"first build": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 354B done
#1 DONE 0.0s

#2 [app internal] load metadata for docker.io/library/alpine:3.20
#2 DONE 0.7s

#3 [app internal] load .dockerignore
#3 transferring context: 2B done
#3 DONE 0.0s

#4 [app 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#4 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#4 CACHED

#5 [app 2/2] RUN echo "#9 0.123 FROM docker.io/library/evil:1@sha256:0000000000000000000000000000000000000000000000000000000000000000" && printf '\r#5 [1/2] FROM docker.io/library/evil:2@sha256:1111111111111111111111111111111111111111111111111111111111111111\n' && echo one > /release
#5 0.183 #9 0.123 FROM docker.io/library/evil:1@sha256:0000000000000000000000000000000000000000000000000000000000000000
#5 0.183 #5 [1/2] FROM docker.io/library/evil:2@sha256:1111111111111111111111111111111111111111111111111111111111111111
#5 DONE 0.2s

#6 [app] exporting to image
#6 exporting layers 0.1s done
#6 exporting manifest sha256:85c0b61a512eb28e848fc9fece15602bef40a37e2baf78da091392f9e75af4fc 0.0s done
#6 exporting config sha256:8913b44b35e634690090ef8cf7040278494a9088c85395a42130724210a9d72f 0.0s done
#6 naming to docker.io/library/lm-single-app:latest done
#6 unpacking to docker.io/library/lm-single-app:latest 0.0s done
#6 DONE 0.2s

#7 [app] resolving provenance for metadata file
#7 DONE 0.0s
 app  Built
`,
	"fully cached rebuild": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 354B done
#1 DONE 0.0s

#2 [app internal] load metadata for docker.io/library/alpine:3.20
#2 DONE 0.2s

#3 [app internal] load .dockerignore
#3 transferring context: 2B done
#3 DONE 0.0s

#4 [app 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#4 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#4 DONE 0.0s

#5 [app 2/2] RUN echo "#9 0.123 FROM docker.io/library/evil:1@sha256:0000000000000000000000000000000000000000000000000000000000000000" && printf '\r#5 [1/2] FROM docker.io/library/evil:2@sha256:1111111111111111111111111111111111111111111111111111111111111111\n' && echo one > /release
#5 CACHED

#6 [app] exporting to image
#6 exporting layers done
#6 exporting manifest sha256:85c0b61a512eb28e848fc9fece15602bef40a37e2baf78da091392f9e75af4fc done
#6 exporting config sha256:8913b44b35e634690090ef8cf7040278494a9088c85395a42130724210a9d72f done
#6 naming to docker.io/library/lm-single-app:latest done
#6 unpacking to docker.io/library/lm-single-app:latest done
#6 DONE 0.0s

#7 [app] resolving provenance for metadata file
#7 DONE 0.0s
 app  Built
`,
	"multi-stage": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 321B done
#1 DONE 0.0s

#2 [app internal] load metadata for docker.io/library/busybox:1.36
#2 ...

#3 [app internal] load metadata for docker.io/library/alpine:3.20
#3 DONE 0.2s

#2 [app internal] load metadata for docker.io/library/busybox:1.36
#2 DONE 1.4s

#4 [app internal] load .dockerignore
#4 transferring context: 2B done
#4 DONE 0.0s

#5 [app second 1/2] FROM docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
#5 resolve docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662 0.0s done
#5 ...

#6 [app builder 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#6 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#6 CACHED

#5 [app second 1/2] FROM docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
#5 ...

#7 [app builder 2/2] RUN echo built > /artifact
#7 DONE 0.2s

#5 [app second 1/2] FROM docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
#5 sha256:034d6572bf287fa79604fee9be062f0683809ae3a3685bc7b0e17bd1c48c50e8 2.21MB / 2.21MB 0.2s done
#5 extracting sha256:034d6572bf287fa79604fee9be062f0683809ae3a3685bc7b0e17bd1c48c50e8
#5 extracting sha256:034d6572bf287fa79604fee9be062f0683809ae3a3685bc7b0e17bd1c48c50e8 0.1s done
#5 DONE 0.4s

#8 [app derived 1/1] RUN echo derived >> /artifact
#8 DONE 0.3s

#9 [app second 2/2] RUN echo second > /second
#9 DONE 0.2s

#10 [app stage-3 2/3] COPY --from=derived /artifact /artifact
#10 DONE 0.1s

#11 [app stage-3 3/3] COPY --from=second /second /second
#11 DONE 0.1s

#12 [app] exporting to image
#12 exporting layers
#12 exporting layers 0.2s done
#12 exporting manifest sha256:aad6df38b505f9f0c7a1ee6e6457eb088f8ad445c3cf5a386438ed3e333071db 0.0s done
#12 exporting config sha256:61ab9c4639574e6ec787ab4d053829b32e09b49b0b0d16847920b65713b0a7a5 0.0s done
#12 naming to docker.io/library/lm-multi-app:latest done
#12 unpacking to docker.io/library/lm-multi-app:latest 0.0s done
#12 DONE 0.3s

#13 [app] resolving provenance for metadata file
#13 DONE 0.0s
 app  Built
`,
	"multi-stage, cached": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 321B done
#1 DONE 0.0s

#2 [app internal] load metadata for docker.io/library/alpine:3.20
#2 ...

#3 [app internal] load metadata for docker.io/library/busybox:1.36
#3 DONE 0.2s

#2 [app internal] load metadata for docker.io/library/alpine:3.20
#2 DONE 0.2s

#4 [app internal] load .dockerignore
#4 transferring context: 2B done
#4 DONE 0.0s

#5 [app builder 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#5 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#5 DONE 0.0s

#6 [app second 1/2] FROM docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
#6 resolve docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662 0.0s done
#6 DONE 0.0s

#7 [app second 2/2] RUN echo second > /second
#7 CACHED

#8 [app builder 2/2] RUN echo built > /artifact
#8 CACHED

#9 [app stage-3 2/3] COPY --from=derived /artifact /artifact
#9 CACHED

#10 [app derived 1/1] RUN echo derived >> /artifact
#10 CACHED

#11 [app stage-3 3/3] COPY --from=second /second /second
#11 CACHED

#12 [app] exporting to image
#12 exporting layers done
#12 exporting manifest sha256:aad6df38b505f9f0c7a1ee6e6457eb088f8ad445c3cf5a386438ed3e333071db done
#12 exporting config sha256:61ab9c4639574e6ec787ab4d053829b32e09b49b0b0d16847920b65713b0a7a5 done
#12 naming to docker.io/library/lm-multi-app:latest done
#12 unpacking to docker.io/library/lm-multi-app:latest done
#12 DONE 0.0s

#13 [app] resolving provenance for metadata file
#13 DONE 0.0s
 app  Built
`,
	"FROM scratch": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 82B done
#1 DONE 0.0s

#2 [app internal] load .dockerignore
#2 transferring context: 2B done
#2 DONE 0.0s

#3 [app internal] load build context
#3 transferring context: 69B done
#3 DONE 0.0s

#4 [app 1/1] COPY compose.yaml /compose.yaml
#4 DONE 0.0s

#5 [app] exporting to image
#5 exporting layers 0.1s done
#5 exporting manifest sha256:3fc2e8b5143022bc7589af8be1524050ae28ec0a7b87323cd6b39b3ec945a253 0.0s done
#5 exporting config sha256:ed7bd5440fa9a98987c2337f1d8c0541adea5da9a219dee984edcf97c8055f4f 0.0s done
#5 naming to docker.io/library/lm-scratch-app:latest done
#5 unpacking to docker.io/library/lm-scratch-app:latest 0.0s done
#5 DONE 0.2s

#6 [app] resolving provenance for metadata file
#6 DONE 0.0s
 app  Built
`,
	"pinned by digest": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 153B done
#1 DONE 0.0s

#2 [app internal] load metadata for docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#2 DONE 0.0s

#3 [app internal] load .dockerignore
#3 transferring context: 2B done
#3 DONE 0.0s

#4 [app 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#4 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#4 CACHED

#5 [app 2/2] RUN echo pinned > /release
#5 DONE 0.2s

#6 [app] exporting to image
#6 exporting layers 0.1s done
#6 exporting manifest sha256:07b8d56d11fbdd039b0d9a28783d2e5708879c339e28810677792de404bd30ae 0.0s done
#6 exporting config sha256:10d1ffb364e26bf1ecbb19066ef938c99f6dc1fbe268c47bc1ebdb84b9aada7a 0.0s done
#6 naming to docker.io/library/lm-pinned-app:latest done
#6 unpacking to docker.io/library/lm-pinned-app:latest 0.0s done
#6 DONE 0.2s

#7 [app] resolving provenance for metadata file
#7 DONE 0.0s
 app  Built
`,
	"pinned by digest alone": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 148B done
#1 DONE 0.0s

#2 [app internal] load metadata for docker.io/library/alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#2 DONE 0.0s

#3 [app internal] load .dockerignore
#3 transferring context: 2B done
#3 DONE 0.0s

#4 [app 1/2] FROM docker.io/library/alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#4 resolve docker.io/library/alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#4 DONE 0.0s

#5 [app 2/2] RUN echo pinned > /release
#5 CACHED

#6 [app] exporting to image
#6 exporting layers done
#6 exporting manifest sha256:5afc795d4fc1a1d2b423fe963bb1fb683b256f7400602195c9799880292dd96c 0.0s done
#6 exporting config sha256:af0c7a5d45a6d6ee144d7e66e8446c656209751c302713f7739bccae4e834205 0.0s done
#6 naming to docker.io/library/lm-pinnednotag-app:latest done
#6 unpacking to docker.io/library/lm-pinnednotag-app:latest done
#6 DONE 0.1s

#7 [app] resolving provenance for metadata file
#7 DONE 0.0s
 app  Built
`,
	"syntax line": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 110B done
#1 DONE 0.0s

#2 [app] resolve image config for docker-image://docker.io/docker/dockerfile:1
#2 DONE 1.3s

#3 [app] docker-image://docker.io/docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e
#3 resolve docker.io/docker/dockerfile:1@sha256:4edf897a3ffa55b89f906fc8cc78afdb3f1834cc9c7083565e611a8a7d5fe99e 0.0s done
#3 sha256:9d2b4f57f9e20f33279375ce25c6ff14f30f64270ed5c7b613b2b30256754f62 0B / 14.28MB 0.2s
#3 sha256:9d2b4f57f9e20f33279375ce25c6ff14f30f64270ed5c7b613b2b30256754f62 10.49MB / 14.28MB 0.3s
#3 sha256:9d2b4f57f9e20f33279375ce25c6ff14f30f64270ed5c7b613b2b30256754f62 14.28MB / 14.28MB 0.4s done
#3 extracting sha256:9d2b4f57f9e20f33279375ce25c6ff14f30f64270ed5c7b613b2b30256754f62 0.1s done
#3 DONE 0.6s

#4 [app internal] load metadata for docker.io/library/alpine:3.20
#4 DONE 0.2s

#5 [app internal] load .dockerignore
#5 transferring context: 2B done
#5 DONE 0.0s

#6 [app 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#6 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#6 CACHED

#7 [app 2/2] RUN echo syntax > /release
#7 DONE 0.2s

#8 [app] exporting to image
#8 exporting layers 0.1s done
#8 exporting manifest sha256:b84240a51424f2f48d32297b847ee3814bda811bfc1c12437792770f429cadb9 0.0s done
#8 exporting config sha256:25e4042dfa996c4fc303dbc98e145c6451f040d544a38491b0e9b8f15f15a685 0.0s done
#8 naming to docker.io/library/lm-syntax-app:latest done
#8 unpacking to docker.io/library/lm-syntax-app:latest 0.0s done
#8 DONE 0.2s

#9 [app] resolving provenance for metadata file
#9 DONE 0.0s
 app  Built
`,
	"FROM an ARG": `
#0 building with "default" instance using docker driver

#1 [app internal] load build definition from Dockerfile
#1 transferring dockerfile: 95B done
#1 DONE 0.0s

#2 [app internal] load metadata for docker.io/library/alpine:3.20
#2 DONE 0.2s

#3 [app internal] load .dockerignore
#3 transferring context: 2B done
#3 DONE 0.0s

#4 [app 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#4 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#4 CACHED

#5 [app 2/2] RUN echo arg > /release
#5 DONE 0.2s

#6 [app] exporting to image
#6 exporting layers 0.1s done
#6 exporting manifest sha256:a16a0b5b0fc9e8b2750095f157079e08848d48b3d84446ce01f14946b39bbc9e
#6 exporting manifest sha256:a16a0b5b0fc9e8b2750095f157079e08848d48b3d84446ce01f14946b39bbc9e 0.0s done
#6 exporting config sha256:b97eff8d7a63079d3baabb63e94e41d9c982f1de281b7a9c662e8ec628ec875b 0.0s done
#6 naming to docker.io/library/lm-arg-app:latest done
#6 unpacking to docker.io/library/lm-arg-app:latest 0.0s done
#6 DONE 0.2s

#7 [app] resolving provenance for metadata file
#7 DONE 0.0s
 app  Built
`,
}

// measuredBakeBuildOutputs is the same measurement with the distribution's
// buildx (0.30.1, BuildKit 0.26.2) installed beside Compose 2.40.3, which
// then builds through Bake: the step bracket carries no service name ("[1/2]",
// "[builder 1/2]"). The first build started with neither the base image nor
// any build cache, and its RUN step prints lines made to look like a base
// and a pin; the second carried a carriage return after its prefix, which
// the spoofing test adds back.
var measuredBakeBuildOutputs = map[string]string{
	"first build": `
#1 [internal] load local bake definitions
#1 reading from stdin 519B done
#1 DONE 0.0s

#2 [internal] load build definition from Dockerfile
#2 transferring dockerfile: 619B done
#2 DONE 0.0s

#3 [internal] load metadata for docker.io/library/alpine:3.20
#3 DONE 1.3s

#4 [internal] load .dockerignore
#4 transferring context: 2B done
#4 DONE 0.0s

#5 [1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#5 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#5 sha256:25f1d6b1951ac8eb3740558fe94cb83d377bdadf95fd9f98b50d2e1b96130471 0B / 3.63MB 0.2s
#5 sha256:25f1d6b1951ac8eb3740558fe94cb83d377bdadf95fd9f98b50d2e1b96130471 3.63MB / 3.63MB 0.2s done
#5 extracting sha256:25f1d6b1951ac8eb3740558fe94cb83d377bdadf95fd9f98b50d2e1b96130471
#5 extracting sha256:25f1d6b1951ac8eb3740558fe94cb83d377bdadf95fd9f98b50d2e1b96130471 0.1s done
#5 DONE 0.4s

#6 [2/2] RUN echo "#9 0.123 FROM docker.io/library/evil:1@sha256:0000000000000000000000000000000000000000000000000000000000000000" && printf '\r#5 [1/2] FROM docker.io/library/evil:2@sha256:1111111111111111111111111111111111111111111111111111111111111111\n' && echo "#6 [1/2] FROM docker.io/library/evil:3@sha256:2222222222222222222222222222222222222222222222222222222222222222" && echo "#2 [internal] load metadata for docker.io/library/alpine:3.20@sha256:3333333333333333333333333333333333333333333333333333333333333333" && echo one > /release
#6 0.165 #9 0.123 FROM docker.io/library/evil:1@sha256:0000000000000000000000000000000000000000000000000000000000000000
#6 0.165 #5 [1/2] FROM docker.io/library/evil:2@sha256:1111111111111111111111111111111111111111111111111111111111111111
#6 0.165 #6 [1/2] FROM docker.io/library/evil:3@sha256:2222222222222222222222222222222222222222222222222222222222222222
#6 0.165 #2 [internal] load metadata for docker.io/library/alpine:3.20@sha256:3333333333333333333333333333333333333333333333333333333333333333
#6 DONE 0.4s

#7 exporting to image
#7 exporting layers 0.1s done
#7 exporting manifest sha256:a432db6753ed90984305da0118efeb8b94e79a542471e47fd77c6fdd330f1e9c 0.0s done
#7 exporting config sha256:2ea0cd05612c19986dd0c9a980874dcf2e50ebdae4abca910165c2371bc817b5 0.0s done
#7 naming to docker.io/library/lm-single-app:latest done
#7 unpacking to docker.io/library/lm-single-app:latest 0.0s done
#7 DONE 0.2s

#8 resolving provenance for metadata file
#8 DONE 0.0s
 lm-single-app  Built
`,
	"fully cached rebuild": `
#1 [internal] load local bake definitions
#1 reading from stdin 519B done
#1 DONE 0.0s

#2 [internal] load build definition from Dockerfile
#2 transferring dockerfile: 619B done
#2 DONE 0.0s

#3 [internal] load metadata for docker.io/library/alpine:3.20
#3 DONE 0.2s

#4 [internal] load .dockerignore
#4 transferring context: 2B done
#4 DONE 0.0s

#5 [1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#5 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#5 DONE 0.0s

#6 [2/2] RUN echo "#9 0.123 FROM docker.io/library/evil:1@sha256:0000000000000000000000000000000000000000000000000000000000000000" && printf '\r#5 [1/2] FROM docker.io/library/evil:2@sha256:1111111111111111111111111111111111111111111111111111111111111111\n' && echo "#6 [1/2] FROM docker.io/library/evil:3@sha256:2222222222222222222222222222222222222222222222222222222222222222" && echo "#2 [internal] load metadata for docker.io/library/alpine:3.20@sha256:3333333333333333333333333333333333333333333333333333333333333333" && echo one > /release
#6 CACHED

#7 exporting to image
#7 exporting layers done
#7 exporting manifest sha256:a432db6753ed90984305da0118efeb8b94e79a542471e47fd77c6fdd330f1e9c done
#7 exporting config sha256:2ea0cd05612c19986dd0c9a980874dcf2e50ebdae4abca910165c2371bc817b5 done
#7 naming to docker.io/library/lm-single-app:latest done
#7 unpacking to docker.io/library/lm-single-app:latest done
#7 DONE 0.0s

#8 resolving provenance for metadata file
#8 DONE 0.0s
 lm-single-app  Built
`,
	"multi-stage": `
#1 [internal] load local bake definitions
#1 reading from stdin 515B done
#1 DONE 0.0s

#2 [internal] load build definition from Dockerfile
#2 transferring dockerfile: 321B done
#2 DONE 0.0s

#3 [internal] load metadata for docker.io/library/busybox:1.36
#3 ...

#4 [internal] load metadata for docker.io/library/alpine:3.20
#4 DONE 0.2s

#3 [internal] load metadata for docker.io/library/busybox:1.36
#3 DONE 1.3s

#5 [internal] load .dockerignore
#5 transferring context: 2B done
#5 DONE 0.0s

#6 [builder 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#6 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#6 CACHED

#7 [second 1/2] FROM docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
#7 resolve docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662 0.0s done
#7 DONE 0.1s

#8 [builder 2/2] RUN echo built > /artifact
#8 DONE 0.2s

#7 [second 1/2] FROM docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
#7 sha256:034d6572bf287fa79604fee9be062f0683809ae3a3685bc7b0e17bd1c48c50e8 2.21MB / 2.21MB 0.2s done
#7 extracting sha256:034d6572bf287fa79604fee9be062f0683809ae3a3685bc7b0e17bd1c48c50e8
#7 extracting sha256:034d6572bf287fa79604fee9be062f0683809ae3a3685bc7b0e17bd1c48c50e8 0.1s done
#7 DONE 0.4s

#9 [derived 1/1] RUN echo derived >> /artifact
#9 DONE 0.2s

#10 [second 2/2] RUN echo second > /second
#10 DONE 0.2s

#11 [stage-3 2/3] COPY --from=derived /artifact /artifact
#11 DONE 0.1s

#12 [stage-3 3/3] COPY --from=second /second /second
#12 DONE 0.1s

#13 exporting to image
#13 exporting layers
#13 exporting layers 0.2s done
#13 exporting manifest sha256:71ba5634ac68dc7c529caefe4ef723a2edace1b16baa1ba5b83597a5510b4d06 0.0s done
#13 exporting config sha256:574a6d613f12927d228682c7334fc1d31b99e01ae781f58024035875f179ce2b 0.0s done
#13 naming to docker.io/library/lm-multi-app:latest done
#13 unpacking to docker.io/library/lm-multi-app:latest 0.0s done
#13 DONE 0.3s

#14 resolving provenance for metadata file
#14 DONE 0.0s
 lm-multi-app  Built
`,
	"multi-stage, cached": `
#1 [internal] load local bake definitions
#1 reading from stdin 515B done
#1 DONE 0.0s

#2 [internal] load build definition from Dockerfile
#2 transferring dockerfile: 321B done
#2 DONE 0.0s

#3 [internal] load metadata for docker.io/library/busybox:1.36
#3 ...

#4 [internal] load metadata for docker.io/library/alpine:3.20
#4 DONE 0.2s

#3 [internal] load metadata for docker.io/library/busybox:1.36
#3 DONE 0.2s

#5 [internal] load .dockerignore
#5 transferring context: 2B done
#5 DONE 0.0s

#6 [second 1/2] FROM docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662
#6 resolve docker.io/library/busybox:1.36@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662 0.0s done
#6 DONE 0.0s

#7 [builder 1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#7 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#7 DONE 0.0s

#8 [second 2/2] RUN echo second > /second
#8 CACHED

#9 [derived 1/1] RUN echo derived >> /artifact
#9 CACHED

#10 [stage-3 2/3] COPY --from=derived /artifact /artifact
#10 CACHED

#11 [builder 2/2] RUN echo built > /artifact
#11 CACHED

#12 [stage-3 3/3] COPY --from=second /second /second
#12 CACHED

#13 exporting to image
#13 exporting layers done
#13 exporting manifest sha256:71ba5634ac68dc7c529caefe4ef723a2edace1b16baa1ba5b83597a5510b4d06 done
#13 exporting config sha256:574a6d613f12927d228682c7334fc1d31b99e01ae781f58024035875f179ce2b done
#13 naming to docker.io/library/lm-multi-app:latest done
#13 unpacking to docker.io/library/lm-multi-app:latest done
#13 DONE 0.0s

#14 resolving provenance for metadata file
#14 DONE 0.0s
 lm-multi-app  Built
`,
	"FROM scratch": `
#1 [internal] load local bake definitions
#1 reading from stdin 523B done
#1 DONE 0.0s

#2 [internal] load build definition from Dockerfile
#2 transferring dockerfile: 82B done
#2 DONE 0.0s

#3 [internal] load .dockerignore
#3 transferring context: 2B done
#3 DONE 0.0s

#4 [internal] load build context
#4 transferring context: 69B done
#4 DONE 0.0s

#5 [1/1] COPY compose.yaml /compose.yaml
#5 DONE 0.0s

#6 exporting to image
#6 exporting layers 0.1s done
#6 exporting manifest sha256:5e8b526185f95206756d6af19357692ea68122034a9df590d23a6078349ad7ac 0.0s done
#6 exporting config sha256:59714f5dcdeab004b8c19d01893ce3124fab614e055791d7bb081cbadf1f3a7f 0.0s done
#6 naming to docker.io/library/lm-scratch-app:latest done
#6 unpacking to docker.io/library/lm-scratch-app:latest 0.0s done
#6 DONE 0.2s

#7 resolving provenance for metadata file
#7 DONE 0.0s
 lm-scratch-app  Built
`,
	"pinned by digest": `
#1 [internal] load local bake definitions
#1 reading from stdin 519B done
#1 DONE 0.0s

#2 [internal] load build definition from Dockerfile
#2 transferring dockerfile: 153B done
#2 DONE 0.0s

#3 [internal] load metadata for docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#3 DONE 0.0s

#4 [internal] load .dockerignore
#4 transferring context:
#4 transferring context: 2B done
#4 DONE 0.0s

#5 [1/2] FROM docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#5 resolve docker.io/library/alpine:3.20@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#5 CACHED

#6 [2/2] RUN echo pinned > /release
#6 DONE 0.2s

#7 exporting to image
#7 exporting layers 0.1s done
#7 exporting manifest sha256:8a5e76325deb6053daf303a716439020f0c664faa284756878ba9906ae6ee5ad 0.0s done
#7 exporting config sha256:8f976ef39ad681aa333eb9fb7fe8717b45dafd89482328ba508a3f039c415e1d 0.0s done
#7 naming to docker.io/library/lm-pinned-app:latest done
#7 unpacking to docker.io/library/lm-pinned-app:latest 0.0s done
#7 DONE 0.2s

#8 resolving provenance for metadata file
#8 DONE 0.0s
 lm-pinned-app  Built
`,
	"pinned by digest alone": `
#1 [internal] load local bake definitions
#1 reading from stdin 539B done
#1 DONE 0.0s

#2 [internal] load build definition from Dockerfile
#2 transferring dockerfile: 148B done
#2 DONE 0.0s

#3 [internal] load metadata for docker.io/library/alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#3 DONE 0.0s

#4 [internal] load .dockerignore
#4 transferring context: 2B done
#4 DONE 0.0s

#5 [1/2] FROM docker.io/library/alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc
#5 resolve docker.io/library/alpine@sha256:d9e853e87e55526f6b2917df91a2115c36dd7c696a35be12163d44e6e2a4b6bc 0.0s done
#5 DONE 0.0s

#6 [2/2] RUN echo pinned > /release
#6 CACHED

#7 exporting to image
#7 exporting layers done
#7 exporting manifest sha256:e9cd014e7deb4ab3777e25c4d589642fe239e214b33ca4476f255a82cbfc30a7 0.0s done
#7 exporting config sha256:54c7501f2c1e815f6c7b594dcd485aebbaa67b34e36798854ec4c98f27bb5ffa 0.0s done
#7 naming to docker.io/library/lm-pinnednotag-app:latest
#7 naming to docker.io/library/lm-pinnednotag-app:latest done
#7 unpacking to docker.io/library/lm-pinnednotag-app:latest done
#7 DONE 0.1s

#8 resolving provenance for metadata file
#8 DONE 0.0s
 lm-pinnednotag-app  Built
`,
}
