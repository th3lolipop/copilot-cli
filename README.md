## AWS Copilot CLI (preview) ![latest version](https://img.shields.io/github/v/release/aws/copilot-cli) [![Join the chat at https://gitter.im/aws/copilot-cli](https://badges.gitter.im/aws/copilot-cli.svg)](https://gitter.im/aws/copilot-cli?utm_source=badge&utm_medium=badge&utm_campaign=pr-badge&utm_content=badge)
###### _Develop, Release and Operate Container Apps on AWS._ 



* **Documentation**: [https://aws.github.io/copilot-cli/](https://aws.github.io/copilot-cli/)

The AWS Copilot CLI is a tool for developers to create, release and manage production ready containerized applications on Amazon ECS and AWS Fargate.
From getting started, pushing to a test environment and releasing to production, Copilot helps you through the entire life of your app development.

Got a Dockerfile and some code? Get it up and running on ECS in under 10 minutes, with just one command. Ready to take that app to production? Spin up new environments and a continuous delivery pipeline without having to leave your terminal. Find a bug? Tail your logs and deploy with one tool.

Use Copilot to:
* Organize all your related micro-services in one application
* Set up test and production environments, across regions and accounts
* Set up production-ready, scalable ECS services and infrastructure
* Set up CI/CD Pipelines for all of the micro-services
* Monitor and debug your services from your terminal

Read more about the Copilot charter and tenets [here](CHARTER.md).

![copilot help menu](https://user-images.githubusercontent.com/879348/90834852-087aeb80-e300-11ea-8340-7f64326036fc.png)

> Using the instructions and assets in this repository folder is governed as a preview program under the AWS Service Terms (https://aws.amazon.com/service-terms/).

## Installing

### Homebrew 🍻

```sh
$ brew install aws/tap/copilot-cli
```

### Manually 
We're distributing binaries from our GitHub releases.

<details>
  <summary>Instructions for installing Copilot for your platform</summary>


| Platform | Command to install |
|---------|---------
| macOS | `curl -Lo /usr/local/bin/copilot https://github.com/aws/copilot-cli/releases/latest/download/copilot-darwin && chmod +x /usr/local/bin/copilot && copilot --help` |
| Linux x86 (64-bit) | `curl -Lo /usr/local/bin/copilot https://github.com/aws/copilot-cli/releases/latest/download/copilot-linux && chmod +x /usr/local/bin/copilot && copilot --help` |
| Linux ARM | `curl -Lo /usr/local/bin/copilot https://github.com/aws/copilot-cli/releases/latest/download/copilot-linux-arm64 && chmod +x /usr/local/bin/copilot && copilot --help` |
| Windows | `Invoke-WebRequest -OutFile 'C:\Program Files\copilot.exe' https://github.com/aws/copilot-cli/releases/latest/download/copilot-windows.exe` |

</details>


## Getting started 🌱

Make sure you have the AWS command line tool installed and have already run `aws configure` before you start.

To get a sample app up and running in one command, run the following:

```sh
$ git clone git@github.com:aws-samples/aws-copilot-sample-service.git demo-app
$ cd demo-app
$ copilot init --app demo                \
  --name api                             \
  --type 'Load Balanced Web Service'     \
  --dockerfile './Dockerfile'            \
  --deploy
```

This will create a VPC, Application Load Balancer, an Amazon ECS Service with the sample app running on AWS Fargate. This process will take around 8 minutes to complete - at which point you'll get a URL for your sample app running!

## Cleaning up 🧹

Once you're finished playing around with this project, you can delete it and all the AWS resources associated it by running `copilot app delete`.

## Running a one-off task 🏃‍♀️

Copilot makes it easy to set up production-ready applications, but you can also use it to run tasks on AWS Fargate without having to set up any infrastructure. To run a simple hello world task on Fargate you can use Copilot:

```sh
$ copilot task run      \
  --default             \
  --follow              \
  --image alpine:latest \
  --command "echo 'Howdy, Copilot'"
```

## Learning more 📖

Want to learn more about what's happening? Check out our documentation [https://aws.github.io/copilot-cli/](https://aws.github.io/copilot-cli/) for a getting started guide, learning about Copilot concepts, and a breakdown of our commands. 

## We need your feedback 🙏

The AWS Copilot CLI is in beta, meaning that you can expect our command names to be stable as well as the shape of our 
infrastructure patterns. However, we want to know what works, what doesn't work, and what you want! 
Have any feedback at all? Drop us an [issue](https://github.com/aws/copilot-cli/issues/new) or join us on [gitter](https://gitter.im/aws/copilot-cli).

We're happy to hear feedback or answer questions, so reach out, anytime!

## Security disclosures

If you think you’ve found a potential security issue, please do not post it in the Issues. Instead, please follow the instructions [here](https://aws.amazon.com/security/vulnerability-reporting/) or email AWS security directly at [aws-security@amazon.com](mailto:aws-security@amazon.com).

## License
This library is licensed under the Apache 2.0 License.
