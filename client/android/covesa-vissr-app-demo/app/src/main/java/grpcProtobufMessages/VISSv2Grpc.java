package grpcProtobufMessages;

import static io.grpc.MethodDescriptor.generateFullMethodName;

/**
 */
@javax.annotation.Generated(
    value = "by gRPC proto compiler (version 1.47.0)",
    comments = "Source: VISSv2.proto")
@io.grpc.stub.annotations.GrpcGenerated
public final class VISSv2Grpc {

  private VISSv2Grpc() {}

  public static final String SERVICE_NAME = "grpcProtobufMessages.VISSv2";

  // Static method descriptors that strictly reflect the proto.
  private static volatile io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage,
      grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage> getGetRequestMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "GetRequest",
      requestType = grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage.class,
      responseType = grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage,
      grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage> getGetRequestMethod() {
    io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage, grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage> getGetRequestMethod;
    if ((getGetRequestMethod = VISSv2Grpc.getGetRequestMethod) == null) {
      synchronized (VISSv2Grpc.class) {
        if ((getGetRequestMethod = VISSv2Grpc.getGetRequestMethod) == null) {
          VISSv2Grpc.getGetRequestMethod = getGetRequestMethod =
              io.grpc.MethodDescriptor.<grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage, grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "GetRequest"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.lite.ProtoLiteUtils.marshaller(
                  grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.lite.ProtoLiteUtils.marshaller(
                  grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage.getDefaultInstance()))
              .build();
        }
      }
    }
    return getGetRequestMethod;
  }

  private static volatile io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage,
      grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage> getSetRequestMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "SetRequest",
      requestType = grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage.class,
      responseType = grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage,
      grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage> getSetRequestMethod() {
    io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage, grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage> getSetRequestMethod;
    if ((getSetRequestMethod = VISSv2Grpc.getSetRequestMethod) == null) {
      synchronized (VISSv2Grpc.class) {
        if ((getSetRequestMethod = VISSv2Grpc.getSetRequestMethod) == null) {
          VISSv2Grpc.getSetRequestMethod = getSetRequestMethod =
              io.grpc.MethodDescriptor.<grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage, grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "SetRequest"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.lite.ProtoLiteUtils.marshaller(
                  grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.lite.ProtoLiteUtils.marshaller(
                  grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage.getDefaultInstance()))
              .build();
        }
      }
    }
    return getSetRequestMethod;
  }

  private static volatile io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage,
      grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage> getSubscribeRequestMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "SubscribeRequest",
      requestType = grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage.class,
      responseType = grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage.class,
      methodType = io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
  public static io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage,
      grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage> getSubscribeRequestMethod() {
    io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage, grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage> getSubscribeRequestMethod;
    if ((getSubscribeRequestMethod = VISSv2Grpc.getSubscribeRequestMethod) == null) {
      synchronized (VISSv2Grpc.class) {
        if ((getSubscribeRequestMethod = VISSv2Grpc.getSubscribeRequestMethod) == null) {
          VISSv2Grpc.getSubscribeRequestMethod = getSubscribeRequestMethod =
              io.grpc.MethodDescriptor.<grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage, grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.SERVER_STREAMING)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "SubscribeRequest"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.lite.ProtoLiteUtils.marshaller(
                  grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.lite.ProtoLiteUtils.marshaller(
                  grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage.getDefaultInstance()))
              .build();
        }
      }
    }
    return getSubscribeRequestMethod;
  }

  private static volatile io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage,
      grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage> getUnsubscribeRequestMethod;

  @io.grpc.stub.annotations.RpcMethod(
      fullMethodName = SERVICE_NAME + '/' + "UnsubscribeRequest",
      requestType = grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage.class,
      responseType = grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage.class,
      methodType = io.grpc.MethodDescriptor.MethodType.UNARY)
  public static io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage,
      grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage> getUnsubscribeRequestMethod() {
    io.grpc.MethodDescriptor<grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage, grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage> getUnsubscribeRequestMethod;
    if ((getUnsubscribeRequestMethod = VISSv2Grpc.getUnsubscribeRequestMethod) == null) {
      synchronized (VISSv2Grpc.class) {
        if ((getUnsubscribeRequestMethod = VISSv2Grpc.getUnsubscribeRequestMethod) == null) {
          VISSv2Grpc.getUnsubscribeRequestMethod = getUnsubscribeRequestMethod =
              io.grpc.MethodDescriptor.<grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage, grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage>newBuilder()
              .setType(io.grpc.MethodDescriptor.MethodType.UNARY)
              .setFullMethodName(generateFullMethodName(SERVICE_NAME, "UnsubscribeRequest"))
              .setSampledToLocalTracing(true)
              .setRequestMarshaller(io.grpc.protobuf.lite.ProtoLiteUtils.marshaller(
                  grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage.getDefaultInstance()))
              .setResponseMarshaller(io.grpc.protobuf.lite.ProtoLiteUtils.marshaller(
                  grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage.getDefaultInstance()))
              .build();
        }
      }
    }
    return getUnsubscribeRequestMethod;
  }

  /**
   * Creates a new async stub that supports all call types for the service
   */
  public static VISSv2Stub newStub(io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<VISSv2Stub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<VISSv2Stub>() {
        @java.lang.Override
        public VISSv2Stub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new VISSv2Stub(channel, callOptions);
        }
      };
    return VISSv2Stub.newStub(factory, channel);
  }

  /**
   * Creates a new blocking-style stub that supports unary and streaming output calls on the service
   */
  public static VISSv2BlockingStub newBlockingStub(
      io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<VISSv2BlockingStub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<VISSv2BlockingStub>() {
        @java.lang.Override
        public VISSv2BlockingStub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new VISSv2BlockingStub(channel, callOptions);
        }
      };
    return VISSv2BlockingStub.newStub(factory, channel);
  }

  /**
   * Creates a new ListenableFuture-style stub that supports unary calls on the service
   */
  public static VISSv2FutureStub newFutureStub(
      io.grpc.Channel channel) {
    io.grpc.stub.AbstractStub.StubFactory<VISSv2FutureStub> factory =
      new io.grpc.stub.AbstractStub.StubFactory<VISSv2FutureStub>() {
        @java.lang.Override
        public VISSv2FutureStub newStub(io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
          return new VISSv2FutureStub(channel, callOptions);
        }
      };
    return VISSv2FutureStub.newStub(factory, channel);
  }

  /**
   */
  public static abstract class VISSv2ImplBase implements io.grpc.BindableService {

    /**
     */
    public void getRequest(grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage request,
        io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getGetRequestMethod(), responseObserver);
    }

    /**
     */
    public void setRequest(grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage request,
        io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getSetRequestMethod(), responseObserver);
    }

    /**
     */
    public void subscribeRequest(grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage request,
        io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getSubscribeRequestMethod(), responseObserver);
    }

    /**
     */
    public void unsubscribeRequest(grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage request,
        io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage> responseObserver) {
      io.grpc.stub.ServerCalls.asyncUnimplementedUnaryCall(getUnsubscribeRequestMethod(), responseObserver);
    }

    @java.lang.Override public final io.grpc.ServerServiceDefinition bindService() {
      return io.grpc.ServerServiceDefinition.builder(getServiceDescriptor())
          .addMethod(
            getGetRequestMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage,
                grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage>(
                  this, METHODID_GET_REQUEST)))
          .addMethod(
            getSetRequestMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage,
                grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage>(
                  this, METHODID_SET_REQUEST)))
          .addMethod(
            getSubscribeRequestMethod(),
            io.grpc.stub.ServerCalls.asyncServerStreamingCall(
              new MethodHandlers<
                grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage,
                grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage>(
                  this, METHODID_SUBSCRIBE_REQUEST)))
          .addMethod(
            getUnsubscribeRequestMethod(),
            io.grpc.stub.ServerCalls.asyncUnaryCall(
              new MethodHandlers<
                grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage,
                grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage>(
                  this, METHODID_UNSUBSCRIBE_REQUEST)))
          .build();
    }
  }

  /**
   */
  public static final class VISSv2Stub extends io.grpc.stub.AbstractAsyncStub<VISSv2Stub> {
    private VISSv2Stub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected VISSv2Stub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new VISSv2Stub(channel, callOptions);
    }

    /**
     */
    public void getRequest(grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage request,
        io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getGetRequestMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     */
    public void setRequest(grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage request,
        io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getSetRequestMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     */
    public void subscribeRequest(grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage request,
        io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage> responseObserver) {
      io.grpc.stub.ClientCalls.asyncServerStreamingCall(
          getChannel().newCall(getSubscribeRequestMethod(), getCallOptions()), request, responseObserver);
    }

    /**
     */
    public void unsubscribeRequest(grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage request,
        io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage> responseObserver) {
      io.grpc.stub.ClientCalls.asyncUnaryCall(
          getChannel().newCall(getUnsubscribeRequestMethod(), getCallOptions()), request, responseObserver);
    }
  }

  /**
   */
  public static final class VISSv2BlockingStub extends io.grpc.stub.AbstractBlockingStub<VISSv2BlockingStub> {
    private VISSv2BlockingStub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected VISSv2BlockingStub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new VISSv2BlockingStub(channel, callOptions);
    }

    /**
     */
    public grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage getRequest(grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getGetRequestMethod(), getCallOptions(), request);
    }

    /**
     */
    public grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage setRequest(grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getSetRequestMethod(), getCallOptions(), request);
    }

    /**
     */
    public java.util.Iterator<grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage> subscribeRequest(
        grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage request) {
      return io.grpc.stub.ClientCalls.blockingServerStreamingCall(
          getChannel(), getSubscribeRequestMethod(), getCallOptions(), request);
    }

    /**
     */
    public grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage unsubscribeRequest(grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage request) {
      return io.grpc.stub.ClientCalls.blockingUnaryCall(
          getChannel(), getUnsubscribeRequestMethod(), getCallOptions(), request);
    }
  }

  /**
   */
  public static final class VISSv2FutureStub extends io.grpc.stub.AbstractFutureStub<VISSv2FutureStub> {
    private VISSv2FutureStub(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      super(channel, callOptions);
    }

    @java.lang.Override
    protected VISSv2FutureStub build(
        io.grpc.Channel channel, io.grpc.CallOptions callOptions) {
      return new VISSv2FutureStub(channel, callOptions);
    }

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage> getRequest(
        grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getGetRequestMethod(), getCallOptions()), request);
    }

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage> setRequest(
        grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getSetRequestMethod(), getCallOptions()), request);
    }

    /**
     */
    public com.google.common.util.concurrent.ListenableFuture<grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage> unsubscribeRequest(
        grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage request) {
      return io.grpc.stub.ClientCalls.futureUnaryCall(
          getChannel().newCall(getUnsubscribeRequestMethod(), getCallOptions()), request);
    }
  }

  private static final int METHODID_GET_REQUEST = 0;
  private static final int METHODID_SET_REQUEST = 1;
  private static final int METHODID_SUBSCRIBE_REQUEST = 2;
  private static final int METHODID_UNSUBSCRIBE_REQUEST = 3;

  private static final class MethodHandlers<Req, Resp> implements
      io.grpc.stub.ServerCalls.UnaryMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ServerStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.ClientStreamingMethod<Req, Resp>,
      io.grpc.stub.ServerCalls.BidiStreamingMethod<Req, Resp> {
    private final VISSv2ImplBase serviceImpl;
    private final int methodId;

    MethodHandlers(VISSv2ImplBase serviceImpl, int methodId) {
      this.serviceImpl = serviceImpl;
      this.methodId = methodId;
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public void invoke(Req request, io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        case METHODID_GET_REQUEST:
          serviceImpl.getRequest((grpcProtobufMessages.VISSv2OuterClass.GetRequestMessage) request,
              (io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.GetResponseMessage>) responseObserver);
          break;
        case METHODID_SET_REQUEST:
          serviceImpl.setRequest((grpcProtobufMessages.VISSv2OuterClass.SetRequestMessage) request,
              (io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.SetResponseMessage>) responseObserver);
          break;
        case METHODID_SUBSCRIBE_REQUEST:
          serviceImpl.subscribeRequest((grpcProtobufMessages.VISSv2OuterClass.SubscribeRequestMessage) request,
              (io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage>) responseObserver);
          break;
        case METHODID_UNSUBSCRIBE_REQUEST:
          serviceImpl.unsubscribeRequest((grpcProtobufMessages.VISSv2OuterClass.UnsubscribeRequestMessage) request,
              (io.grpc.stub.StreamObserver<grpcProtobufMessages.VISSv2OuterClass.UnsubscribeResponseMessage>) responseObserver);
          break;
        default:
          throw new AssertionError();
      }
    }

    @java.lang.Override
    @java.lang.SuppressWarnings("unchecked")
    public io.grpc.stub.StreamObserver<Req> invoke(
        io.grpc.stub.StreamObserver<Resp> responseObserver) {
      switch (methodId) {
        default:
          throw new AssertionError();
      }
    }
  }

  private static volatile io.grpc.ServiceDescriptor serviceDescriptor;

  public static io.grpc.ServiceDescriptor getServiceDescriptor() {
    io.grpc.ServiceDescriptor result = serviceDescriptor;
    if (result == null) {
      synchronized (VISSv2Grpc.class) {
        result = serviceDescriptor;
        if (result == null) {
          serviceDescriptor = result = io.grpc.ServiceDescriptor.newBuilder(SERVICE_NAME)
              .addMethod(getGetRequestMethod())
              .addMethod(getSetRequestMethod())
              .addMethod(getSubscribeRequestMethod())
              .addMethod(getUnsubscribeRequestMethod())
              .build();
        }
      }
    }
    return result;
  }
}
