package com.vissr.safetyscore.ui

import com.vissr.safetyscore.domain.getConnectionStatusUseCase
import com.vissr.safetyscore.domain.subscribeRequestUseCase
import com.vissr.safetyscore.ui.mapper.DomainViewDataPackageMapper
import com.vissr.safetyscore.ui.mapper.build
import org.koin.androidx.viewmodel.dsl.viewModel
import org.koin.dsl.module

val vissApiGrpcDemoKoinModule = module {
    viewModel {
        VissApiGrpcDemoViewModel(
            domainViewDataPackageMapper = get(),
            subscribeRequestUseCase = subscribeRequestUseCase,
            getConnectionStatusUseCase = getConnectionStatusUseCase,
        )
    }

    factory {
        DomainViewDataPackageMapper.build()
    }
}
