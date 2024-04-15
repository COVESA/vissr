package com.vissr.safetyscore.cloud

import android.util.Log
import grpcProtobufMessages.VISSv2GrpcKt
import io.grpc.ConnectivityState
import io.grpc.ManagedChannelBuilder
import kotlinx.coroutines.Runnable

interface GrpcVissApiFactory {

    fun getCarApiGrpcStub(): VISSv2GrpcKt.VISSv2CoroutineStub

    fun setUpChannelConnectionCallback(callback: Runnable)

    companion object Factory
}

fun GrpcVissApiFactory.Factory.build(): GrpcVissApiFactory = GrpcVissApiFactoryImpl()

class GrpcVissApiFactoryImpl : GrpcVissApiFactory {

    private val channel = setUpCarApiGrpcChannel()

    override fun getCarApiGrpcStub(): VISSv2GrpcKt.VISSv2CoroutineStub {
        return VISSv2GrpcKt.VISSv2CoroutineStub(channel)
    }

    override fun setUpChannelConnectionCallback(callback: Runnable) {
        channel.notifyWhenStateChanged(ConnectivityState.READY) {
            Log.d("GrpcVissApiFactory", "Channel state changed to READY")
            callback.run()
        }
    }

    private fun setUpCarApiGrpcChannel() =
        ManagedChannelBuilder.forAddress("localhost", 8887)
            .usePlaintext()
            .build()
}


