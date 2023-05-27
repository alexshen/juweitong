(function (module) {
    function showErrorTip(msg) {
        weui.topTips(msg);
    }

    const IGNORE_ERROR = new Error();

    function request(url, { success, error = showErrorTip, ...options }) {
        fetch(url, options)
            .then(function (response) {
                if (!response.ok) {
                    if (response.status === 401) {
                        console.error('session expired');
                        weui.toast('请重新登入', { callback() {
                            window.location.href = '/qr_login';
                        }});
                        return Promise.reject(IGNORE_ERROR);
                    }
                    return Promise.reject(`请求错误, ${response.statusText}`);
                }
                return response.json();
            })
            .then(function (result) {
                if (!result.success) {
                    return Promise.reject(`请求错误, ${result.err}`);
                }
                success?.(result.data);
            })
            .catch(function (e) {
                if (e !== IGNORE_ERROR) {
                    error(e);
                }
            });
    }

    module.common = {
        request,
    };
})(window);
