package com.vissr.safetyscore.domain.repository

import com.vissr.safetyscore.domain.model.VISSv2DataPackages
import kotlinx.coroutines.flow.Flow

interface VissApiRepository {

    fun subscribeRequest(): Flow<VISSv2DataPackages?>

    fun getConnectionStatus(): Flow<Boolean>

    fun sendAGTRequest(): Flow<String>

    fun sendATRequest(agttoken:String): Flow<String>

    companion object Factory
}
