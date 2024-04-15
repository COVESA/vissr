package com.vissr.safetyscore.domain

import com.vissr.safetyscore.domain.model.VISSv2DataPackages
import com.vissr.safetyscore.domain.repository.VissApiRepository
import kotlinx.coroutines.flow.Flow
import org.koin.core.scope.Scope

typealias SubscribeRequestUseCase = () -> Flow<VISSv2DataPackages?>
val Scope.subscribeRequestUseCase: SubscribeRequestUseCase
    get() = vissApiRepository::subscribeRequest

typealias GetConnectionStatusUseCase = () -> Flow<Boolean>
val Scope.getConnectionStatusUseCase: GetConnectionStatusUseCase
    get() = vissApiRepository::getConnectionStatus

private val Scope.vissApiRepository: VissApiRepository get() = get()
