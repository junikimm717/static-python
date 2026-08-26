#include <algorithm>
#include <iostream>
#include <numeric>
#include <string>
#include <vector>

int main() {
	std::vector<int> v{5, 1, 4, 2, 3};
	std::sort(v.begin(), v.end());

	std::string out = "cxx-hello sorted=";
	for (size_t i = 0; i < v.size(); i++) {
		if (i) out += ",";
		out += std::to_string(v[i]);
	}

	long long s = std::accumulate(v.begin(), v.end(), 0LL);
	out += " sum=" + std::to_string(s);

	std::cout << out << std::endl;
	return (s == 15) ? 0 : 2;
}
