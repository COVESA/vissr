package grpcProtobufMessages

import grpcProtobufMessages.VISSv2Grpc.getServiceDescriptor
import io.grpc.CallOptions
import io.grpc.CallOptions.DEFAULT
import io.grpc.Channel
import io.grpc.Metadata
import io.grpc.MethodDescriptor
import io.grpc.ServerServiceDefinition
import io.grpc.ServerServiceDefinition.builder
import io.grpc.ServiceDescriptor
import io.grpc.Status
import io.grpc.Status.UNIMPLEMENTED
import io.grpc.StatusException
import io.grpc.kotlin.AbstractCoroutineServerImpl
import io.grpc.kotlin.AbstractCoroutineStub
import io.grpc.kotlin.ClientCalls
import io.grpc.kotlin.ClientCalls.serverStreamingRpc
import io.grpc.kotlin.ClientCalls.unaryRpc
import io.grpc.kotlin.ServerCalls
import io.grpc.kotlin.ServerCalls.serverStreamingServerMethodDefinition
import io.grpc.kotlin.ServerCalls.unaryServerMethodDefinition
import io.grpc.kotlin.StubFor
import kotlin.String
import kotlin.coroutines.CoroutineContext
import kotlin.coroutines.EmptyCoroutineContext
import kotlin.jvm.JvmOverloads
import kotlin.jvm.JvmStatic
import kotlinx.coroutines.flow.Flow

/**
 * Holder for Kotlin coroutine-based client and server APIs for grpcProtobufMessages.VISSv2.
 */
public object VISSv2GrpcKt {
  public const val SERVICE_NAME: String = VISSv2Grpc.SERVICE_NAME

  @JvmStatic
  public val serviceDescriptor: ServiceDescriptor
    get() = VISSv2Grpc.getServiceDescriptor()

  public val getRequestMethod:
      MethodDescriptor<VISSv2OuterClass.GetRequestMessage, VISSv2OuterClass.GetResponseMessage>
    @JvmStatic
    get() = VISSv2Grpc.getGetRequestMethod()

  public val setRequestMethod:
      MethodDescriptor<VISSv2OuterClass.SetRequestMessage, VISSv2OuterClass.SetResponseMessage>
    @JvmStatic
    get() = VISSv2Grpc.getSetRequestMethod()

  public val subscribeRequestMethod:
      MethodDescriptor<VISSv2OuterClass.SubscribeRequestMessage, VISSv2OuterClass.SubscribeStreamMessage>
    @JvmStatic
    get() = VISSv2Grpc.getSubscribeRequestMethod()

  public val unsubscribeRequestMethod:
      MethodDescriptor<VISSv2OuterClass.UnsubscribeRequestMessage, VISSv2OuterClass.UnsubscribeResponseMessage>
    @JvmStatic
    get() = VISSv2Grpc.getUnsubscribeRequestMethod()

  /**
   * A stub for issuing RPCs to a(n) grpcProtobufMessages.VISSv2 service as suspending coroutines.
   */
  @StubFor(VISSv2Grpc::class)
  public class VISSv2CoroutineStub @JvmOverloads constructor(
    channel: Channel,
    callOptions: CallOptions = DEFAULT,
  ) : AbstractCoroutineStub<VISSv2CoroutineStub>(channel, callOptions) {
    public override fun build(channel: Channel, callOptions: CallOptions): VISSv2CoroutineStub =
        VISSv2CoroutineStub(channel, callOptions)

    /**
     * Executes this RPC and returns the response message, suspending until the RPC completes
     * with [`Status.OK`][Status].  If the RPC completes with another status, a corresponding
     * [StatusException] is thrown.  If this coroutine is cancelled, the RPC is also cancelled
     * with the corresponding exception as a cause.
     *
     * @param request The request message to send to the server.
     *
     * @param headers Metadata to attach to the request.  Most users will not need this.
     *
     * @return The single response from the server.
     */
    public suspend fun getRequest(request: VISSv2OuterClass.GetRequestMessage, headers: Metadata =
        Metadata()): VISSv2OuterClass.GetResponseMessage = unaryRpc(
      channel,
      VISSv2Grpc.getGetRequestMethod(),
      request,
      callOptions,
      headers
    )

    /**
     * Executes this RPC and returns the response message, suspending until the RPC completes
     * with [`Status.OK`][Status].  If the RPC completes with another status, a corresponding
     * [StatusException] is thrown.  If this coroutine is cancelled, the RPC is also cancelled
     * with the corresponding exception as a cause.
     *
     * @param request The request message to send to the server.
     *
     * @param headers Metadata to attach to the request.  Most users will not need this.
     *
     * @return The single response from the server.
     */
    public suspend fun setRequest(request: VISSv2OuterClass.SetRequestMessage, headers: Metadata =
        Metadata()): VISSv2OuterClass.SetResponseMessage = unaryRpc(
      channel,
      VISSv2Grpc.getSetRequestMethod(),
      request,
      callOptions,
      headers
    )

    /**
     * Returns a [Flow] that, when collected, executes this RPC and emits responses from the
     * server as they arrive.  That flow finishes normally if the server closes its response with
     * [`Status.OK`][Status], and fails by throwing a [StatusException] otherwise.  If
     * collecting the flow downstream fails exceptionally (including via cancellation), the RPC
     * is cancelled with that exception as a cause.
     *
     * @param request The request message to send to the server.
     *
     * @param headers Metadata to attach to the request.  Most users will not need this.
     *
     * @return A flow that, when collected, emits the responses from the server.
     */
    public fun subscribeRequest(request: VISSv2OuterClass.SubscribeRequestMessage, headers: Metadata
        = Metadata()): Flow<VISSv2OuterClass.SubscribeStreamMessage> = serverStreamingRpc(
      channel,
      VISSv2Grpc.getSubscribeRequestMethod(),
      request,
      callOptions,
      headers
    )

    /**
     * Executes this RPC and returns the response message, suspending until the RPC completes
     * with [`Status.OK`][Status].  If the RPC completes with another status, a corresponding
     * [StatusException] is thrown.  If this coroutine is cancelled, the RPC is also cancelled
     * with the corresponding exception as a cause.
     *
     * @param request The request message to send to the server.
     *
     * @param headers Metadata to attach to the request.  Most users will not need this.
     *
     * @return The single response from the server.
     */
    public suspend fun unsubscribeRequest(request: VISSv2OuterClass.UnsubscribeRequestMessage,
        headers: Metadata = Metadata()): VISSv2OuterClass.UnsubscribeResponseMessage = unaryRpc(
      channel,
      VISSv2Grpc.getUnsubscribeRequestMethod(),
      request,
      callOptions,
      headers
    )
  }

  /**
   * Skeletal implementation of the grpcProtobufMessages.VISSv2 service based on Kotlin coroutines.
   */
  public abstract class VISSv2CoroutineImplBase(
    coroutineContext: CoroutineContext = EmptyCoroutineContext,
  ) : AbstractCoroutineServerImpl(coroutineContext) {
    /**
     * Returns the response to an RPC for grpcProtobufMessages.VISSv2.GetRequest.
     *
     * If this method fails with a [StatusException], the RPC will fail with the corresponding
     * [Status].  If this method fails with a [java.util.concurrent.CancellationException], the RPC
     * will fail
     * with status `Status.CANCELLED`.  If this method fails for any other reason, the RPC will
     * fail with `Status.UNKNOWN` with the exception as a cause.
     *
     * @param request The request from the client.
     */
    public open suspend fun getRequest(request: VISSv2OuterClass.GetRequestMessage):
        VISSv2OuterClass.GetResponseMessage = throw
        StatusException(UNIMPLEMENTED.withDescription("Method grpcProtobufMessages.VISSv2.GetRequest is unimplemented"))

    /**
     * Returns the response to an RPC for grpcProtobufMessages.VISSv2.SetRequest.
     *
     * If this method fails with a [StatusException], the RPC will fail with the corresponding
     * [Status].  If this method fails with a [java.util.concurrent.CancellationException], the RPC
     * will fail
     * with status `Status.CANCELLED`.  If this method fails for any other reason, the RPC will
     * fail with `Status.UNKNOWN` with the exception as a cause.
     *
     * @param request The request from the client.
     */
    public open suspend fun setRequest(request: VISSv2OuterClass.SetRequestMessage):
        VISSv2OuterClass.SetResponseMessage = throw
        StatusException(UNIMPLEMENTED.withDescription("Method grpcProtobufMessages.VISSv2.SetRequest is unimplemented"))

    /**
     * Returns a [Flow] of responses to an RPC for grpcProtobufMessages.VISSv2.SubscribeRequest.
     *
     * If creating or collecting the returned flow fails with a [StatusException], the RPC
     * will fail with the corresponding [Status].  If it fails with a
     * [java.util.concurrent.CancellationException], the RPC will fail with status
     * `Status.CANCELLED`.  If creating
     * or collecting the returned flow fails for any other reason, the RPC will fail with
     * `Status.UNKNOWN` with the exception as a cause.
     *
     * @param request The request from the client.
     */
    public open fun subscribeRequest(request: VISSv2OuterClass.SubscribeRequestMessage):
        Flow<VISSv2OuterClass.SubscribeStreamMessage> = throw
        StatusException(UNIMPLEMENTED.withDescription("Method grpcProtobufMessages.VISSv2.SubscribeRequest is unimplemented"))

    /**
     * Returns the response to an RPC for grpcProtobufMessages.VISSv2.UnsubscribeRequest.
     *
     * If this method fails with a [StatusException], the RPC will fail with the corresponding
     * [Status].  If this method fails with a [java.util.concurrent.CancellationException], the RPC
     * will fail
     * with status `Status.CANCELLED`.  If this method fails for any other reason, the RPC will
     * fail with `Status.UNKNOWN` with the exception as a cause.
     *
     * @param request The request from the client.
     */
    public open suspend fun unsubscribeRequest(request: VISSv2OuterClass.UnsubscribeRequestMessage):
        VISSv2OuterClass.UnsubscribeResponseMessage = throw
        StatusException(UNIMPLEMENTED.withDescription("Method grpcProtobufMessages.VISSv2.UnsubscribeRequest is unimplemented"))

    public final override fun bindService(): ServerServiceDefinition =
        builder(getServiceDescriptor())
      .addMethod(unaryServerMethodDefinition(
      context = this.context,
      descriptor = VISSv2Grpc.getGetRequestMethod(),
      implementation = ::getRequest
    ))
      .addMethod(unaryServerMethodDefinition(
      context = this.context,
      descriptor = VISSv2Grpc.getSetRequestMethod(),
      implementation = ::setRequest
    ))
      .addMethod(serverStreamingServerMethodDefinition(
      context = this.context,
      descriptor = VISSv2Grpc.getSubscribeRequestMethod(),
      implementation = ::subscribeRequest
    ))
      .addMethod(unaryServerMethodDefinition(
      context = this.context,
      descriptor = VISSv2Grpc.getUnsubscribeRequestMethod(),
      implementation = ::unsubscribeRequest
    )).build()
  }
}
