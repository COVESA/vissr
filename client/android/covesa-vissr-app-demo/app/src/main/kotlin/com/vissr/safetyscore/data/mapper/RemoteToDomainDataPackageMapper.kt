package com.vissr.safetyscore.data.mapper

import android.util.Log
import com.vissr.safetyscore.domain.model.VISSv2DataPackage
import com.vissr.safetyscore.domain.model.VISSv2DataPackages
import com.vissr.safetyscore.domain.model.VISSv2DataPoint
import com.vissr.safetyscore.utils.mapper.Mapper
import grpcProtobufMessages.VISSv2OuterClass.DataPackages.DataPackage
import grpcProtobufMessages.VISSv2OuterClass.SubscribeStreamMessage

fun interface RemoteToDomainDataPackageMapper : Mapper<SubscribeStreamMessage, VISSv2DataPackages?> {

    companion object Factory
}

internal fun RemoteToDomainDataPackageMapper.Factory.build(): RemoteToDomainDataPackageMapper =
    RemoteToDomainDataPackageMapperImpl()

private class RemoteToDomainDataPackageMapperImpl : RemoteToDomainDataPackageMapper {

    override fun map(value: SubscribeStreamMessage): VISSv2DataPackages? =
        if (value.hasEvent() && value.event.hasSuccessResponse()) {
            VISSv2DataPackages(
                dataPackage = value
                    .event
                    .successResponse
                    .dataPack
                    .dataList
                    .map { it.toCarApiDataPackage() }
            )
        } else {
            if (value.hasResponse()) {
                Log.d("CarApiMapper", "map: has response ${value.response}")
            } else if (value.hasEvent() && value.event.hasErrorResponse()) {
                Log.d("CarApiMapper", "map: has error response ${value.event.errorResponse}")
            } else {
                Log.d("CarApiMapper", "map: has no response or error response")
            }
            null
        }

    private fun DataPackage.toCarApiDataPackage() =
        VISSv2DataPackage(
            path = path,
            dataPoints = this.dpList.map { it.toCarApiDataPoint() }
        )

    private fun DataPackage.DataPoint.toCarApiDataPoint() =
        VISSv2DataPoint(
            value = value,
            timestamp = ts
        )
}
