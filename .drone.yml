local Pipeline(name, image) = {
  kind: "pipeline",
  name: name,
  steps: [
    {
      name: "test",
      image: image,
      commands: [
        "go vet",
        "go test -v -cover"
      ]
    }
  ]
};

[
  Pipeline("go-1-11", "golang:1.11"),
  Pipeline("go-1-12", "golang:1.12"),
]
