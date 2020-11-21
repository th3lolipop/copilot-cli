// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0
"use strict";

describe("Env Controller Handler", () => {
  const AWS = require("aws-sdk-mock");
  const sinon = require("sinon");
  const EnvController = require("../lib/env-controller");
  const LambdaTester = require("lambda-tester").noVersionCheck();
  const nock = require("nock");
  const ResponseURL = "https://cloudwatch-response-mock.example.com/";
  const testRequestId = "f4ef1b10-c39a-44e3-99c0-fbf7e53c3943";
  let origLog = console.log;

  const testEnvStack = "mockEnvStack";
  const testWorkload = "mockWorkload";
  const testParams = [
    {
      ParameterKey: "ALBWorkloads",
      ParameterValue: "my-app,my-other-app",
    },
  ];
  const testOutputs = [
    {
      OutputKey: "CFNExecutionRoleARN",
      OutputValue:
        "arn:aws:iam::1234567890:role/my-project-prod-CFNExecutionRole",
    },
  ];

  beforeEach(() => {
    EnvController.withDefaultResponseURL(ResponseURL);
    EnvController.deadlineExpired = function () {
      return new Promise(function (resolve, reject) {});
    };
    // Prevent logging.
    console.log = function () {};
  });
  afterEach(() => {
    // Restore logger
    AWS.restore();
    console.log = origLog;
  });

  test("invalid operation", () => {
    const request = nock(ResponseURL)
      .put("/", (body) => {
        return (
          body.Status === "FAILED" &&
          body.Reason === "Unsupported request type OOPS"
        );
      })
      .reply(200);

    return LambdaTester(EnvController.handler)
      .event({
        RequestType: "OOPS",
        RequestId: testRequestId,
        ResponseURL: ResponseURL,
        ResourceProperties: {
          EnvStack: testEnvStack,
          Workload: testWorkload,
          Parameters: ["ALBWorkloads"],
        },
      })
      .expectResolve(() => {
        expect(request.isDone()).toBe(true);
      });
  });

  test("fail if cannot find environment stack", () => {
    const describeStacksFake = sinon.fake.resolves({
      Stacks: [],
    });
    AWS.mock("CloudFormation", "describeStacks", describeStacksFake);
    const request = nock(ResponseURL)
      .put("/", (body) => {
        return (
          body.Status === "FAILED" &&
          body.Reason === "Cannot find environment stack mockEnvStack"
        );
      })
      .reply(200);

    return LambdaTester(EnvController.handler)
      .event({
        RequestType: "Create",
        RequestId: testRequestId,
        ResponseURL: ResponseURL,
        ResourceProperties: {
          EnvStack: testEnvStack,
          Workload: testWorkload,
          Parameters: ["ALBWorkloads"],
        },
      })
      .expectResolve(() => {
        sinon.assert.calledWith(
          describeStacksFake,
          sinon.match({
            StackName: "mockEnvStack",
          })
        );
        expect(request.isDone()).toBe(true);
      });
  });

  test("unexpected update stack error", () => {
    const describeStacksFake = sinon.fake.resolves({
      Stacks: [
        {
          StackName: "mockEnvStack",
          Parameters: testParams,
          Outputs: [],
        },
      ],
    });
    AWS.mock("CloudFormation", "describeStacks", describeStacksFake);
    const updateStackFake = sinon.fake.throws(new Error("not apple pie"));
    AWS.mock("CloudFormation", "updateStack", updateStackFake);
    const request = nock(ResponseURL)
      .put("/", (body) => {
        return body.Status === "FAILED" && body.Reason === "not apple pie";
      })
      .reply(200);

    return LambdaTester(EnvController.handler)
      .event({
        RequestType: "Create",
        RequestId: testRequestId,
        ResponseURL: ResponseURL,
        ResourceProperties: {
          EnvStack: testEnvStack,
          Workload: testWorkload,
          Parameters: ["ALBWorkloads"],
        },
      })
      .expectResolve(() => {
        sinon.assert.calledWith(
          describeStacksFake,
          sinon.match({
            StackName: "mockEnvStack",
          })
        );
        sinon.assert.calledWith(
          updateStackFake,
          sinon.match({
            Parameters: [
              {
                ParameterKey: "ALBWorkloads",
                ParameterValue: "my-app,my-other-app,mockWorkload",
              },
            ],
            StackName: "mockEnvStack",
            UsePreviousTemplate: true,
          })
        );
        expect(request.isDone()).toBe(true);
      });
  });

  test("Return early if nothing changes", () => {
    const describeStacksFake = sinon.fake.resolves({
      Stacks: [
        {
          StackName: "mockEnvStack",
          Parameters: testParams,
          Outputs: testOutputs,
        },
      ],
    });
    AWS.mock("CloudFormation", "describeStacks", describeStacksFake);
    const updateStackFake = sinon.stub();
    AWS.mock("CloudFormation", "updateStack", updateStackFake);

    const request = nock(ResponseURL)
      .put("/", (body) => {
        return (
          body.Status === "SUCCESS" &&
          body.Data.CFNExecutionRoleARN ===
            "arn:aws:iam::1234567890:role/my-project-prod-CFNExecutionRole"
        );
      })
      .reply(200);

    return LambdaTester(EnvController.handler)
      .event({
        RequestType: "Update",
        RequestId: testRequestId,
        ResponseURL: ResponseURL,
        ResourceProperties: {
          EnvStack: testEnvStack,
          Workload: "my-app",
          Parameters: ["ALBWorkloads"],
        },
      })
      .expectResolve(() => {
        sinon.assert.calledWith(
          describeStacksFake,
          sinon.match({
            StackName: "mockEnvStack",
          })
        );
        sinon.assert.notCalled(updateStackFake);
        expect(request.isDone()).toBe(true);
      });
  });

  test("Wait if the stack is updating in progress", () => {
    const describeStacksFake = sinon.fake.resolves({
      Stacks: [
        {
          StackName: "mockEnvStack",
          Parameters: testParams,
          Outputs: [],
        },
      ],
    });
    AWS.mock("CloudFormation", "describeStacks", describeStacksFake);
    const updateStackFake = sinon.stub();
    updateStackFake
      .onFirstCall()
      .throws(
        new Error(
          "Stack mockEnvStack is in UPDATE_IN_PROGRESS state and can not be updated"
        )
      );
    updateStackFake.onSecondCall().resolves(null);
    AWS.mock("CloudFormation", "updateStack", updateStackFake);
    const waitForFake = sinon.stub();
    waitForFake.onFirstCall().resolves(null);
    waitForFake.onSecondCall().resolves(null);
    AWS.mock("CloudFormation", "waitFor", waitForFake);

    const request = nock(ResponseURL)
      .put("/", (body) => {
        return body.Status === "SUCCESS";
      })
      .reply(200);

    return LambdaTester(EnvController.handler)
      .event({
        RequestType: "Update",
        RequestId: testRequestId,
        ResponseURL: ResponseURL,
        ResourceProperties: {
          EnvStack: testEnvStack,
          Workload: testWorkload,
          Parameters: ["ALBWorkloads"],
        },
      })
      .expectResolve(() => {
        sinon.assert.calledWith(
          describeStacksFake,
          sinon.match({
            StackName: "mockEnvStack",
          })
        );
        sinon.assert.calledWith(
          updateStackFake,
          sinon.match({
            Parameters: [
              {
                ParameterKey: "ALBWorkloads",
                ParameterValue: "my-app,my-other-app,mockWorkload",
              },
            ],
            StackName: "mockEnvStack",
            UsePreviousTemplate: true,
          })
        );
        sinon.assert.calledWith(
          waitForFake.firstCall,
          sinon.match("stackUpdateComplete"),
          sinon.match({
            StackName: "mockEnvStack",
          })
        );
        sinon.assert.calledWith(
          waitForFake.secondCall,
          sinon.match("stackUpdateComplete"),
          sinon.match({
            StackName: "mockEnvStack",
          })
        );
        expect(request.isDone()).toBe(true);
      });
  });

  test("Delete successfully", () => {
    const describeStacksFake = sinon.fake.resolves({
      Stacks: [
        {
          StackName: "mockEnvStack",
          Parameters: testParams,
          Outputs: [],
        },
      ],
    });
    AWS.mock("CloudFormation", "describeStacks", describeStacksFake);
    const updateStackFake = sinon.fake.resolves({});
    AWS.mock("CloudFormation", "updateStack", updateStackFake);
    const waitForFake = sinon.fake.resolves({});
    AWS.mock("CloudFormation", "waitFor", waitForFake);

    const request = nock(ResponseURL)
      .put("/", (body) => {
        return body.Status === "SUCCESS";
      })
      .reply(200);

    return LambdaTester(EnvController.handler)
      .event({
        RequestType: "Delete",
        RequestId: testRequestId,
        ResponseURL: ResponseURL,
        ResourceProperties: {
          EnvStack: testEnvStack,
          Workload: "my-app",
          Parameters: ["ALBWorkloads"],
        },
      })
      .expectResolve(() => {
        sinon.assert.calledWith(
          describeStacksFake,
          sinon.match({
            StackName: "mockEnvStack",
          })
        );
        sinon.assert.calledWith(
          updateStackFake,
          sinon.match({
            Parameters: [
              {
                ParameterKey: "ALBWorkloads",
                ParameterValue: "my-other-app",
              },
            ],
            StackName: "mockEnvStack",
            UsePreviousTemplate: true,
          })
        );
        sinon.assert.calledWith(
          waitForFake,
          sinon.match("stackUpdateComplete"),
          sinon.match({
            StackName: "mockEnvStack",
          })
        );
        expect(request.isDone()).toBe(true);
        expect(request.isDone()).toBe(true);
      });
  });
});
