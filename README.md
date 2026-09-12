# go-shopify-graphql-model

This is a simple library to help you use Shopify's GraphQL objects in your Go code.  It comes with a models based on a specific version of the Shopify GraphQL (see [./graph/model/version.go](./graph/model/version.go) for the current version).

* This commit is likely to be abandoned because while working on it the original author also implemented the fetch step in go so it is no longer needed, though it's possible some items could be useful.   I've committed this to my repo just for reference.
* The fetch go was created with the help of ChatGPT to convert the fetchSchema.js to go.

## Using ##

* `import "github.com/r0busta/go-shopify-graphql-model/v5/graph/model"`
  * replace `v5` with the version of the module you want to use


## Updating the Model ##

If you want a version that isn't available or want to grab one specific to your store front, you can fetch and build a model specific for you:

* Generic model: `API_VERSION=2026-07 go run .` (or `go run . all 2026-07`)
* To only fetch the GraphQL schema: `go run . fetch 2026-07`
* To generate the model from the already fetch schema: `go run . generate`
  * This updates graph/model/models_gen.go


### Specify your store ###

    ```bash
    STORE=my-store ACCESS_TOKEN=shpca_xxxxx API_VERSION=2026-07 go run .
    ```


## Files ##

The `files` directory has the intermediate files used for the current model

* `schema.json`: Response from Shopify for the Introspection query about their GraphQL API
* `schema.graphql`: Modified SDL (schema header omitted)
* `apiVersion.txt`: API Version that was used to create `schema.graphql`
