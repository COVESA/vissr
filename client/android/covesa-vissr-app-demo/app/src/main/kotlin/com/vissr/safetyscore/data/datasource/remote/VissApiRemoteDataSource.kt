package com.vissr.safetyscore.data.datasource.remote

import android.util.Log
import com.vissr.safetyscore.cloud.GrpcVissApiFactory
import com.vissr.safetyscore.domain.model.dataPointList
import grpcProtobufMessages.FilterExpressionsKt.filterExpression
import grpcProtobufMessages.VISSv2OuterClass.DataPackages
import grpcProtobufMessages.VISSv2OuterClass.FilterExpressions
import grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage
import grpcProtobufMessages.filterExpressions
import grpcProtobufMessages.subscribeRequestMessage
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.onEach
import kotlinx.coroutines.flow.retry

interface VissApiRemoteDataSource {

    fun subscribeRequest(aToken:String): Flow<SubscribeStreamMessage>

    fun notifyConnectionStatus(callback: () -> Unit)

    companion object Factory
}

internal fun VissApiRemoteDataSource.Factory.build(
    grpcVissApiFactory: GrpcVissApiFactory
): VissApiRemoteDataSource = VissApiRemoteDataSourceImpl(grpcVissApiFactory)

private class VissApiRemoteDataSourceImpl(
    private val grpcVissApiFactory: GrpcVissApiFactory
) : VissApiRemoteDataSource {

    override fun notifyConnectionStatus(callback: () -> Unit) {
        grpcVissApiFactory.setUpChannelConnectionCallback {
            callback()
        }
    }

    override fun subscribeRequest(aToken:String): Flow<SubscribeStreamMessage> {
        Log.d("VissApiRemoteDataSource", "subscribeRequest called")
//        return flow {
//            emit(SubscribeStreamMessage.getDefaultInstance())
//            delay(1000)
//            emit(createSubscribeStreamMessage())
//        }
        return grpcVissApiFactory
            .getCarApiGrpcStub()
            .subscribeRequest(createSubscribeRequest(aToken))
            .retry { cause ->
                if (cause is io.grpc.StatusException) {
                    when (cause.status.code) {
                        io.grpc.Status.Code.UNAVAILABLE -> {
                            delay(1000)
                            Log.d("VissApiRemoteDataSource", "subscribeRequest: retrying")
                            true
                        }
                        else -> false
                    }
                } else {
                    false
                }
            }
            .onEach {
                Log.d("VissApiRemoteDataSource", "subscribeRequest: subscribeStreamMessage = $it")
            }
    }

    private fun createSubscribeRequest(aToken:String) =
        subscribeRequestMessage {
            authorization = aToken
            path = "Vehicle"
            requestId = "249"
            filter = filterExpressions {
                this.filterExp.addAll(
                    listOf(
                        filterExpression {
                            fType = FilterExpressions.FilterExpression.FilterType.TIMEBASED
                            value = FilterExpressions.FilterExpression.FilterValue
                                .newBuilder().apply {
                                    valueTimebased = FilterExpressions.FilterExpression.FilterValue.TimebasedValue
                                        .newBuilder()
                                        .setPeriod("100")
                                        .build()
                                }
                                .build()
                        },
                        filterExpression {
                            fType = FilterExpressions.FilterExpression.FilterType.PATHS
                            value = FilterExpressions.FilterExpression.FilterValue
                                .newBuilder().apply {
                                    valuePaths = FilterExpressions.FilterExpression.FilterValue.PathsValue
                                        .newBuilder()
                                        .addAllRelativePath(dataPointList)
                                        .build()
                                }
                                .build()
                        }
                    )
                )
            }
        }

    private fun createSubscribeStreamMessage() =
        SubscribeStreamMessage.newBuilder()
            .setEvent(
                SubscribeStreamMessage.SubscribeEventMessage
                    .newBuilder()
                    .setSuccessResponse(
                        SubscribeStreamMessage.SubscribeEventMessage.SuccessResponseMessage
                            .newBuilder()
                            .setDataPack(
                                DataPackages.newBuilder()
                                    .addAllData(
                                        listOf(
                                            DataPackages.DataPackage.newBuilder()
                                                .setPath("Vehicle.Speed")
                                                .addDp(
                                                    DataPackages.DataPackage.DataPoint
                                                        .newBuilder()
                                                        .setValue("100")
                                                        .setTs("custom timestamp")
                                                        .build()
                                                )
                                                .build(),
                                            DataPackages.DataPackage.newBuilder()
                                                .setPath("Vehicle.CurrentLocation.Latitude")
                                                .addDp(
                                                    DataPackages.DataPackage.DataPoint
                                                        .newBuilder()
                                                        .setValue("59.334591")
                                                        .setTs("custom timestamp")
                                                        .build()
                                                )
                                                .build(),
                                            DataPackages.DataPackage.newBuilder()
                                                .setPath("Vehicle.CurrentLocation.Longitude")
                                                .addDp(
                                                    DataPackages.DataPackage.DataPoint
                                                        .newBuilder()
                                                        .setValue("18.063240")
                                                        .setTs("custom timestamp")
                                                        .build()
                                                )
                                                .build()
                                        )
                                    )
                                    .build()
                            )
                            .build()
                    )
                    .build()
            )
            .build()
}
