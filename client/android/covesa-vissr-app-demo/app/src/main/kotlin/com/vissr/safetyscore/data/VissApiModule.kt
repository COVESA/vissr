package com.vissr.safetyscore.data

import com.vissr.safetyscore.cloud.GrpcVissApiFactory
import com.vissr.safetyscore.cloud.build
import com.vissr.safetyscore.data.datasource.remote.VissApiRemoteDataSource
import com.vissr.safetyscore.data.datasource.remote.RetrofitRemoteDataSource
import com.vissr.safetyscore.data.datasource.remote.build
import com.vissr.safetyscore.data.datasource.remote.retrofit.AGTClient
import com.vissr.safetyscore.data.datasource.remote.retrofit.ATClient
import com.vissr.safetyscore.data.datasource.remote.retrofit.build
import com.vissr.safetyscore.data.mapper.RemoteToDomainDataPackageMapper
import com.vissr.safetyscore.data.mapper.build
import com.vissr.safetyscore.data.repository.build
import com.vissr.safetyscore.domain.repository.VissApiRepository
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import org.koin.dsl.module

val vissApiDataModule = module {

    single {
        VissApiRepository.Factory.build(
            remoteDataSource = get(),
            retrofitRemoteDataSource = get(),
            remoteToDomainDataPackageMapper = get(),
            coroutineScope = CoroutineScope(Dispatchers.IO)
        )
    }

    single {
        VissApiRemoteDataSource.build(
            grpcVissApiFactory = get(),
        )
    }

    single {
        GrpcVissApiFactory.build()
    }

    single {
        RetrofitRemoteDataSource.build(
            agtClient = get(),
            atClient = get(),
        )
    }

    single {
        AGTClient.build()
    }

    single {
        ATClient.build()
    }

    factory {
        RemoteToDomainDataPackageMapper.build()
    }
}
