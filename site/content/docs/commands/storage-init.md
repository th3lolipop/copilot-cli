# storage init
```bash
$ copilot storage init
```
## What does it do?
`copilot storage init` creates a new storage resource attached to one of your services, accessible from inside your service container via a friendly environment variable. You can specify either *S3* or *DynamoDB* as the resource type.

After running this command, the CLI creates an `addons` subdirectory inside your `copilot/service` directory if it does not exist. When you run `copilot svc deploy`, your newly initialized storage resource is created in the environment you're deploying to. By default, only the service you specify during `storage init` will have access to that storage resource.

## What are the flags?
```bash
Required Flags
  -n, --name string           Name of the storage resource to create.
  -t, --storage-type string   Type of storage to add. Must be one of:
                              "DynamoDB", "S3"
  -s, --svc string            Name of the service to associate with storage.

DynamoDB Flags
      --lsi stringArray        Optional. Attribute to use as an alternate sort key. May be specified up to 5 times.
                               Must be of the format '<keyName>:<dataType>'.
      --no-lsi                 Optional. Don't ask about configuring alternate sort keys.
      --no-sort                Optional. Skip configuring sort keys.
      --partition-key string   Partition key for the DDB table.
                               Must be of the format '<keyName>:<dataType>'.
      --sort-key string        Optional. Sort key for the DDB table.
                               Must be of the format '<keyName>:<dataType>'.
```

## How can I use it? 
Create an S3 bucket named "my-bucket" attached to the "frontend" service.

```
$ copilot storage init -n my-bucket -t S3 -s frontend
```
Create a basic DynamoDB table named "my-table" attached to the "frontend" service with a sort key specified.

```
$ copilot storage init -n my-table -t DynamoDB -s frontend --partition-key Email:S --sort-key UserId:N --no-lsi
```

Create a DynamoDB table with multiple alternate sort keys.

```
$ copilot storage init \
  -n my-table -t DynamoDB -s frontend \
  --partition-key Email:S \
  --sort-key UserId:N \
  --lsi Points:N \
  --lsi Goodness:N
```


## What happens under the hood?
Copilot writes a Cloudformation template specifying the S3 bucket or DDB table to the `addons` dir. When you run `copilot svc deploy`, the CLI merges this template with all the other templates in the addons directory to create a nested stack associated with your service. This nested stack describes all the additional resources you've associated with that service and is deployed wherever your service is deployed. 

This means that after running
```
$ copilot storage init -n bucket -t S3 -s fe
$ copilot svc deploy -n fe -e test
$ copilot svc deploy -n fe -e prod
```
there will be two buckets deployed, one in the "test" env and one in the "prod" env, accessible only to the "fe" service in its respective environment. 